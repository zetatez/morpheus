package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/zetatez/morpheus/internal/session"
	"github.com/zetatez/morpheus/pkg/sdk"
)

type RunStatus string

const (
	RunStatusQueued      RunStatus = "queued"
	RunStatusRunning     RunStatus = "running"
	RunStatusWaitingTool RunStatus = "waiting_tool"
	RunStatusWaitingUser RunStatus = "waiting_user"
	RunStatusReplaying   RunStatus = "replaying"
	RunStatusTimedOut    RunStatus = "timed_out"
	RunStatusCompleted   RunStatus = "completed"
	RunStatusFailed      RunStatus = "failed"
	RunStatusCancelled   RunStatus = "cancelled"
)

type RunEvent struct {
	Seq  int64          `json:"seq"`
	Type string         `json:"type"`
	Data map[string]any `json:"data,omitempty"`
	Time time.Time      `json:"time"`
}

type RunState struct {
	ID                 string
	SessionID          string
	Mode               AgentMode
	Format             *OutputFormat
	Input              UserInput
	Prompt             string
	Status             RunStatus
	Reply              string
	Err                string
	Confirmation       *ConfirmationPayload
	Plan               sdk.Plan
	Results            []sdk.ToolResult
	Todos              []RunTodo
	LastStep           string
	Messages           []map[string]any
	Events             []RunEvent
	NextSeq            int64
	Cancel             context.CancelFunc
	Deadline           time.Time
	ToolTimeout        time.Duration
	CreatedAt          time.Time
	UpdatedAt          time.Time
	Done               bool
	Cancelled          bool
	LastEventSeqServed int64
	Subscribers        map[chan RunEvent]struct{}
	mu                 sync.Mutex
}

type RunTodo struct {
	ID       string `json:"id"`
	Content  string `json:"content"`
	Status   string `json:"status"`
	Priority string `json:"priority,omitempty"`
	Active   bool   `json:"active,omitempty"`
	Tool     string `json:"tool,omitempty"`
	Note     string `json:"note,omitempty"`
}

type runStore struct {
	mu        sync.RWMutex
	runs      map[string]*RunState
	bySession map[string]string
}

func newRunStore() *runStore {
	return &runStore{runs: map[string]*RunState{}, bySession: map[string]string{}}
}

func (s *runStore) create(sessionID string, input UserInput, format *OutputFormat, mode AgentMode) *RunState {
	run := &RunState{
		ID:          uuid.NewString(),
		SessionID:   sessionID,
		Mode:        mode,
		Format:      format,
		Input:       input,
		Status:      RunStatusQueued,
		ToolTimeout: 60 * time.Second,
		Subscribers: map[chan RunEvent]struct{}{},
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	s.mu.Lock()
	s.runs[run.ID] = run
	s.bySession[sessionID] = run.ID
	s.mu.Unlock()
	return run
}

func (s *runStore) latestBySession(sessionID string) (*RunState, bool) {
	s.mu.RLock()
	id, ok := s.bySession[sessionID]
	if !ok {
		s.mu.RUnlock()
		return nil, false
	}
	run, ok := s.runs[id]
	s.mu.RUnlock()
	return run, ok
}

func (s *runStore) cancel(id string) bool {
	s.mu.RLock()
	run, ok := s.runs[id]
	s.mu.RUnlock()
	if !ok {
		return false
	}
	run.mu.Lock()
	defer run.mu.Unlock()
	if run.Done || run.Cancelled {
		return true
	}
	run.Cancelled = true
	if run.Cancel != nil {
		run.Cancel()
	}
	run.appendEvent("run_cancel_requested", map[string]any{"run_id": run.ID})
	return true
}

func (s *runStore) get(id string) (*RunState, bool) {
	s.mu.RLock()
	run, ok := s.runs[id]
	s.mu.RUnlock()
	return run, ok
}

func (r *RunState) appendEvent(eventType string, data map[string]any) RunEvent {
	r.NextSeq++
	evt := RunEvent{Seq: r.NextSeq, Type: eventType, Data: data, Time: time.Now()}
	r.Events = append(r.Events, evt)
	r.UpdatedAt = evt.Time
	for ch := range r.Subscribers {
		select {
		case ch <- evt:
		default:
		}
	}
	return evt
}

func (r *RunState) subscribe() chan RunEvent {
	ch := make(chan RunEvent, 32)
	r.mu.Lock()
	if r.Subscribers == nil {
		r.Subscribers = map[chan RunEvent]struct{}{}
	}
	r.Subscribers[ch] = struct{}{}
	r.mu.Unlock()
	return ch
}

func (r *RunState) unsubscribe(ch chan RunEvent) {
	r.mu.Lock()
	delete(r.Subscribers, ch)
	r.mu.Unlock()
}

func (r *RunState) snapshotResponse() Response {
	return Response{
		RunID:        r.ID,
		RunStatus:    string(r.Status),
		Plan:         r.Plan,
		Results:      r.Results,
		Reply:        r.Reply,
		Confirmation: r.Confirmation,
		Todos:        exportRunTodos(r.Todos),
	}
}

func (rt *Runtime) startRun(sessionID string, input UserInput, format *OutputFormat, mode AgentMode) *RunState {
	if sessionID == "" {
		sessionID = "default"
	}
	run := rt.runs.create(sessionID, input, format, mode)
	run.Deadline = time.Now().Add(5 * time.Minute)
	run.Plan = sdk.Plan{Summary: input.Text, Status: sdk.PlanStatusInProgress}
	run.appendEvent("run_created", map[string]any{"run_id": run.ID, "session_id": sessionID, "mode": mode})
	return run
}

func (rt *Runtime) emitRunEvent(run *RunState, emit replEmitter, eventType string, data map[string]any) error {
	run.mu.Lock()
	evt := run.appendEvent(eventType, data)
	run.mu.Unlock()
	rt.logger.Info("emitRunEvent", zap.String("run_id", run.ID), zap.String("type", eventType), zap.Int64("seq", evt.Seq))
	if rt.sessionStore != nil {
		_ = rt.sessionStore.AppendRunEvent(context.Background(), run.ID, evt.Seq, evt.Type, evt.Data, evt.Time)
		_ = rt.sessionStore.SaveRun(context.Background(), session.StoredRun{
			ID:        run.ID,
			SessionID: run.SessionID,
			Status:    string(run.Status),
			Reply:     run.Reply,
			Error:     run.Err,
			LastStep:  run.LastStep,
			CreatedAt: run.CreatedAt,
			UpdatedAt: run.UpdatedAt,
		})
	}
	payload := map[string]any{"run_id": run.ID, "seq": evt.Seq, "type": evt.Type, "data": evt.Data, "time": evt.Time}
	if emit != nil {
		return emit("run_event", payload)
	}
	return nil
}

func cloneMessages(messages []map[string]any) []map[string]any {
	if len(messages) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(messages))
	for _, msg := range messages {
		data, _ := json.Marshal(msg)
		var cloned map[string]any
		_ = json.Unmarshal(data, &cloned)
		out = append(out, cloned)
	}
	return out
}

func finishRun(run *RunState, status RunStatus, reply string, err error) {
	run.Status = status
	run.Reply = reply
	if err != nil {
		run.Err = err.Error()
	}
	run.Done = true
	run.UpdatedAt = time.Now()
}

func (rt *Runtime) recoverRunsOnStartup(ctx context.Context) {
	if rt.sessionStore == nil {
		return
	}
	runs, err := rt.sessionStore.RecoverUnfinishedRuns(ctx)
	if err != nil {
		rt.logger.Warn("failed to recover unfinished runs", zap.Error(err))
		return
	}
	for _, stored := range runs {
		if stored.Status == string(RunStatusWaitingUser) {
			continue
		}
		run := &RunState{
			ID:        stored.ID,
			SessionID: stored.SessionID,
			Status:    RunStatusTimedOut,
			Reply:     stored.Reply,
			Err:       "server restarted before run completed",
			LastStep:  stored.LastStep,
			CreatedAt: stored.CreatedAt,
			UpdatedAt: time.Now(),
			Done:      true,
		}
		run.appendEvent("run_recovered", map[string]any{"previous_status": stored.Status})
		rt.runs.mu.Lock()
		rt.runs.runs[run.ID] = run
		rt.runs.bySession[run.SessionID] = run.ID
		rt.runs.mu.Unlock()
		_ = rt.sessionStore.SaveRun(ctx, session.StoredRun{
			ID:        run.ID,
			SessionID: run.SessionID,
			Status:    string(run.Status),
			Reply:     run.Reply,
			Error:     run.Err,
			LastStep:  run.LastStep,
			CreatedAt: run.CreatedAt,
			UpdatedAt: run.UpdatedAt,
		})
		_ = rt.sessionStore.MarkRunStatus(ctx, run.ID, string(run.Status), run.Err)
	}
}

func (rt *Runtime) runFinalEvent(run *RunState, emit replEmitter) {
	data := map[string]any{
		"status": statusString(run.Status),
		"reply":  run.Reply,
		"todos":  exportRunTodos(run.Todos),
	}
	if run.Err != "" {
		data["error"] = run.Err
	}
	if run.Confirmation != nil {
		data["confirmation"] = run.Confirmation
	}
	_ = rt.emitRunEvent(run, emit, "run_finished", data)
}

func exportRunTodos(todos []RunTodo) []map[string]any {
	if len(todos) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(todos))
	for _, todo := range todos {
		out = append(out, map[string]any{
			"id":       todo.ID,
			"content":  todo.Content,
			"status":   todo.Status,
			"priority": todo.Priority,
			"active":   todo.Active,
			"tool":     todo.Tool,
			"note":     todo.Note,
		})
	}
	return out
}

func normalizeTodos(raw []map[string]any) []RunTodo {
	if len(raw) == 0 {
		return nil
	}
	out := make([]RunTodo, 0, len(raw))
	seen := map[string]struct{}{}
	for i, item := range raw {
		content := strings.TrimSpace(stringValue(item["content"]))
		if content == "" {
			continue
		}
		id := strings.TrimSpace(stringValue(item["id"]))
		if id == "" {
			id = fmt.Sprintf("todo-%d", i+1)
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		status := normalizeTodoStatus(stringValue(item["status"]))
		priority := normalizeTodoPriority(stringValue(item["priority"]))
		active, _ := item["active"].(bool)
		out = append(out, RunTodo{ID: id, Content: content, Status: status, Priority: priority, Active: active, Tool: stringValue(item["tool"]), Note: stringValue(item["note"])})
	}
	return out
}

func normalizeTodoStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "completed":
		return "completed"
	case "in_progress":
		return "in_progress"
	case "failed":
		return "failed"
	case "cancelled":
		return "cancelled"
	default:
		return "pending"
	}
}

func normalizeTodoPriority(priority string) string {
	switch strings.ToLower(strings.TrimSpace(priority)) {
	case "high", "medium", "low":
		return strings.ToLower(strings.TrimSpace(priority))
	default:
		return "medium"
	}
}

func stringValue(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func (rt *Runtime) updateRunTodos(run *RunState, todos []RunTodo, emit replEmitter) {
	if run == nil {
		return
	}
	next := append([]RunTodo(nil), todos...)
	sort.SliceStable(next, func(i, j int) bool { return next[i].ID < next[j].ID })
	run.mu.Lock()
	run.Todos = next
	run.mu.Unlock()
	_ = rt.emitRunEvent(run, emit, "todos_updated", map[string]any{"todos": exportRunTodos(next)})
}

func statusString(status RunStatus) string { return string(status) }

func (rt *Runtime) replayRunEvents(ctx context.Context, runID string, afterSeq int64, emit replEmitter) error {
	run, ok := rt.runs.get(runID)
	if !ok {
		if rt.sessionStore != nil {
			events, err := rt.sessionStore.LoadRunEventsWindow(ctx, runID, afterSeq, 200)
			if err == nil && len(events) > 0 {
				for _, evt := range events {
					var data map[string]any
					if evt.Data != "" {
						_ = json.Unmarshal([]byte(evt.Data), &data)
					}
					payload := map[string]any{"run_id": runID, "seq": evt.Seq, "type": evt.Type, "data": data, "time": evt.Time}
					if err := emit("run_event", payload); err != nil {
						return err
					}
				}
				return nil
			}
		}
		return fmt.Errorf("run not found")
	}
	run.mu.Lock()
	events := append([]RunEvent(nil), run.Events...)
	resp := run.snapshotResponse()
	run.mu.Unlock()
	if afterSeq > 0 {
		run.mu.Lock()
		run.Status = RunStatusReplaying
		run.mu.Unlock()
	}
	limit := 200
	served := 0
	for _, evt := range events {
		if evt.Seq <= afterSeq {
			continue
		}
		if served >= limit {
			break
		}
		payload := map[string]any{"run_id": run.ID, "seq": evt.Seq, "type": evt.Type, "data": evt.Data, "time": evt.Time}
		if err := emit("run_event", payload); err != nil {
			return err
		}
		served++
		run.LastEventSeqServed = evt.Seq
	}
	if afterSeq > 0 && !run.Done {
		run.mu.Lock()
		run.Status = RunStatusRunning
		run.mu.Unlock()
	}
	if run.Done {
		if err := emit("done", resp); err != nil {
			return err
		}
		return nil
	}
	sub := run.subscribe()
	defer run.unsubscribe(sub)
	for {
		select {
		case <-ctx.Done():
			return nil
		case evt := <-sub:
			if evt.Seq <= afterSeq {
				continue
			}
			payload := map[string]any{"run_id": run.ID, "seq": evt.Seq, "type": evt.Type, "data": evt.Data, "time": evt.Time}
			if err := emit("run_event", payload); err != nil {
				return err
			}
			afterSeq = evt.Seq
			if evt.Type == "run_finished" {
				if err := emit("done", run.snapshotResponse()); err != nil {
					return err
				}
				return nil
			}
		}
	}
	return nil
}

func (s *APIServer) handleRunByID(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/runs/")
	if path == "" {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	runID := parts[0]
	if len(parts) == 1 {
		if r.Method == http.MethodGet && r.URL.Query().Get("latest") == "1" {
			run, ok := s.runtime.runs.latestBySession(runID)
			if !ok && s.runtime.sessionStore != nil {
				stored, err := s.runtime.sessionStore.LoadLatestRunBySession(r.Context(), runID)
				if err == nil {
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(Response{RunID: stored.ID, RunStatus: stored.Status, Reply: stored.Reply})
					return
				}
			}
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "run not found"})
				return
			}
			run.mu.Lock()
			resp := run.snapshotResponse()
			run.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		if r.Method == http.MethodPost && r.URL.Query().Get("action") == "cancel" {
			if !s.runtime.runs.cancel(runID) {
				w.WriteHeader(http.StatusNotFound)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "run not found"})
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "run_id": runID})
			return
		}
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		run, ok := s.runtime.runs.get(runID)
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "run not found"})
			return
		}
		run.mu.Lock()
		resp := run.snapshotResponse()
		run.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
		return
	}
	if len(parts) == 2 && parts[1] == "events" {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		flusher, ok := w.(http.Flusher)
		if !ok {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		afterSeq := int64(0)
		limit := 200
		if raw := strings.TrimSpace(r.URL.Query().Get("after_seq")); raw != "" {
			fmt.Sscanf(raw, "%d", &afterSeq)
		}
		if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
			fmt.Sscanf(raw, "%d", &limit)
			if limit <= 0 || limit > 1000 {
				limit = 200
			}
		}
		_ = limit
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusOK)
		flusher.Flush()
		emit := func(event string, data interface{}) error {
			payload, err := json.Marshal(replStreamEvent{Event: event, Data: data})
			if err != nil {
				return err
			}
			_, err = w.Write([]byte("data: "))
			if err != nil {
				return err
			}
			_, err = w.Write(payload)
			if err != nil {
				return err
			}
			_, err = w.Write([]byte("\n\n"))
			if err != nil {
				return err
			}
			flusher.Flush()
			return nil
		}
		if err := s.runtime.replayRunEvents(r.Context(), runID, afterSeq, emit); err != nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(": close\n\n"))
		flusher.Flush()
		return
	}
	w.WriteHeader(http.StatusNotFound)
}

func (s *APIServer) handleRuns(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		querySession := strings.TrimSpace(r.URL.Query().Get("session"))
		if querySession == "" {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "session is required"})
			return
		}
		status := strings.TrimSpace(r.URL.Query().Get("status"))
		limit := 20
		cursor := strings.TrimSpace(r.URL.Query().Get("cursor"))
		if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
			fmt.Sscanf(raw, "%d", &limit)
		}
		items := []map[string]any{}
		nextCursor := ""
		if s.runtime.sessionStore != nil {
			if storedRuns, next, err := s.runtime.sessionStore.ListRunsBySession(r.Context(), querySession, status, limit, cursor); err == nil {
				for _, run := range storedRuns {
					items = append(items, map[string]any{"run_id": run.ID, "run_status": run.Status, "reply": run.Reply, "updated_at": run.UpdatedAt, "created_at": run.CreatedAt, "last_step": run.LastStep, "error": run.Error})
				}
				nextCursor = next
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"runs": items, "next_cursor": nextCursor})
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	s.runtime.logger.Info("handleRuns POST start")
	var body struct {
		Session     string            `json:"session"`
		Input       string            `json:"input"`
		Attachments []InputAttachment `json:"attachments,omitempty"`
		Format      *OutputFormat     `json:"format,omitempty"`
		Mode        string            `json:"mode,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	mode := s.runtime.defaultAgentMode()
	if strings.TrimSpace(body.Mode) != "" {
		mode = normalizeAgentMode(AgentMode(body.Mode))
	}
	run := s.runtime.startRun(body.Session, UserInput{Text: body.Input, Attachments: body.Attachments}, body.Format, mode)
	s.runtime.logger.Info("handleRuns run created", zap.String("run_id", run.ID), zap.String("session", run.SessionID))
	go func() {
		s.runtime.logger.Info("background run start", zap.String("run_id", run.ID))
		_, _ = s.runtime.runAgentLoopWithRun(context.Background(), run, body.Session, UserInput{Text: body.Input, Attachments: body.Attachments}, body.Format, mode, runnerCallbacks{callChat: s.runtime.callChatWithTools})
	}()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"run_id": run.ID, "run_status": run.Status})
}

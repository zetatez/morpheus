package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

type remoteWSRequest struct {
	Type        string            `json:"type"`
	SessionID   string            `json:"session_id,omitempty"`
	Input       string            `json:"input,omitempty"`
	RunID       string            `json:"run_id,omitempty"`
	Query       string            `json:"query,omitempty"`
	Status      string            `json:"status,omitempty"`
	Cursor      string            `json:"cursor,omitempty"`
	AfterSeq    int64             `json:"after_seq,omitempty"`
	Limit       int               `json:"limit,omitempty"`
	Attachments []InputAttachment `json:"attachments,omitempty"`
	Format      *OutputFormat     `json:"format,omitempty"`
	Mode        string            `json:"mode,omitempty"`
}

type remoteWSMessage struct {
	Type  string         `json:"type"`
	Data  map[string]any `json:"data,omitempty"`
	Error string         `json:"error,omitempty"`
}

var remoteWSUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func (s *APIServer) handleRemoteWS(w http.ResponseWriter, r *http.Request) {
	if !s.runtime.cfg.Server.Remote.Enabled {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if !s.authorizeRemote(r) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
		return
	}
	conn, err := remoteWSUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	clientID := uuid.NewString()
	writeMu := &sync.Mutex{}
	s.logger.Info("remote ws connected", zap.String("client_id", clientID), zap.String("remote", r.RemoteAddr))
	s.writeRemoteWS(writeMu, conn, remoteWSMessage{Type: "ready", Data: map[string]any{"client_id": clientID, "server_time": time.Now().UTC()}})
	for {
		var req remoteWSRequest
		if err := conn.ReadJSON(&req); err != nil {
			return
		}
		if err := s.handleRemoteWSRequest(r.Context(), writeMu, conn, req); err != nil {
			s.writeRemoteWS(writeMu, conn, remoteWSMessage{Type: "error", Error: err.Error()})
		}
	}
}

func (s *APIServer) writeRemoteWS(mu *sync.Mutex, conn *websocket.Conn, msg remoteWSMessage) error {
	mu.Lock()
	defer mu.Unlock()
	return conn.WriteJSON(msg)
}

func (s *APIServer) authorizeRemote(r *http.Request) bool {
	token := strings.TrimSpace(s.runtime.cfg.Server.Remote.BearerToken)
	if token == "" {
		return true
	}
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(auth), "bearer ") {
		return strings.TrimSpace(auth[7:]) == token
	}
	return strings.TrimSpace(r.URL.Query().Get("token")) == token
}

func (s *APIServer) handleRemoteWSRequest(ctx context.Context, writeMu *sync.Mutex, conn *websocket.Conn, req remoteWSRequest) error {
	sessionID := strings.TrimSpace(req.SessionID)
	if sessionID == "" {
		sessionID = "default"
	}
	switch strings.ToLower(strings.TrimSpace(req.Type)) {
	case "ping":
		return s.writeRemoteWS(writeMu, conn, remoteWSMessage{Type: "pong", Data: map[string]any{"time": time.Now().UTC()}})
	case "session.list":
		query := strings.ToLower(strings.TrimSpace(req.Query))
		items := []map[string]any{}
		if s.runtime.sessionStore != nil {
			metas, err := s.runtime.sessionStore.ListSessions(ctx, query)
			if err != nil {
				return err
			}
			for _, meta := range metas {
				items = append(items, map[string]any{"id": meta.SessionID, "updated_at": meta.UpdatedAt, "summary": meta.Summary})
			}
		}
		return s.writeRemoteWS(writeMu, conn, remoteWSMessage{Type: "session.list", Data: map[string]any{"sessions": items}})
	case "session.get":
		messages := s.runtime.conversation.History(ctx, sessionID)
		return s.writeRemoteWS(writeMu, conn, remoteWSMessage{Type: "session", Data: map[string]any{"session_id": sessionID, "messages": messages, "summary": s.runtime.conversation.Summary(sessionID)}})
	case "run.list":
		limit := req.Limit
		if limit <= 0 || limit > 100 {
			limit = 20
		}
		items := []map[string]any{}
		nextCursor := ""
		if s.runtime.sessionStore != nil {
			storedRuns, next, err := s.runtime.sessionStore.ListRunsBySession(ctx, sessionID, strings.TrimSpace(req.Status), limit, strings.TrimSpace(req.Cursor))
			if err != nil {
				return err
			}
			for _, run := range storedRuns {
				items = append(items, map[string]any{"run_id": run.ID, "run_status": run.Status, "reply": run.Reply, "updated_at": run.UpdatedAt, "created_at": run.CreatedAt, "last_step": run.LastStep, "error": run.Error})
			}
			nextCursor = next
		}
		return s.writeRemoteWS(writeMu, conn, remoteWSMessage{Type: "run.list", Data: map[string]any{"session_id": sessionID, "runs": items, "next_cursor": nextCursor}})
	case "run.start":
		if strings.TrimSpace(req.Input) == "" {
			return fmt.Errorf("input is required")
		}
		mode := s.runtime.defaultAgentMode()
		if strings.TrimSpace(req.Mode) != "" {
			mode = normalizeAgentMode(AgentMode(req.Mode))
		}
		run := s.runtime.startRun(sessionID, UserInput{Text: req.Input, Attachments: req.Attachments}, req.Format, mode)
		go func() {
			_, _ = s.runtime.runAgentLoopWithRun(context.Background(), run, sessionID, UserInput{Text: req.Input, Attachments: req.Attachments}, req.Format, mode, runnerCallbacks{callChat: s.runtime.callChatWithTools})
		}()
		return s.writeRemoteWS(writeMu, conn, remoteWSMessage{Type: "run.started", Data: map[string]any{"session_id": sessionID, "run_id": run.ID, "run_status": run.Status}})
	case "run.events":
		if strings.TrimSpace(req.RunID) == "" {
			return fmt.Errorf("run_id is required")
		}
		limit := req.Limit
		if limit <= 0 || limit > 1000 {
			limit = 200
		}
		count := 0
		emit := func(event string, data interface{}) error {
			if count >= limit {
				return nil
			}
			count++
			payload, _ := data.(map[string]any)
			return conn.WriteJSON(remoteWSMessage{Type: event, Data: payload})
		}
		if err := s.runtime.replayRunEvents(ctx, req.RunID, req.AfterSeq, emit); err != nil {
			return err
		}
		return s.writeRemoteWS(writeMu, conn, remoteWSMessage{Type: "run.events.complete", Data: map[string]any{"run_id": req.RunID}})
	case "run.subscribe":
		if strings.TrimSpace(req.RunID) == "" {
			return fmt.Errorf("run_id is required")
		}
		run, ok := s.runtime.runs.get(req.RunID)
		if !ok {
			return fmt.Errorf("run not found")
		}
		sub := run.subscribe()
		defer run.unsubscribe(sub)
		if err := s.runtime.replayRunEvents(ctx, req.RunID, req.AfterSeq, func(event string, data interface{}) error {
			payload, _ := data.(map[string]any)
			return s.writeRemoteWS(writeMu, conn, remoteWSMessage{Type: event, Data: payload})
		}); err != nil {
			return err
		}
		for {
			select {
			case <-ctx.Done():
				return nil
			case evt := <-sub:
				if err := s.writeRemoteWS(writeMu, conn, remoteWSMessage{Type: evt.Type, Data: map[string]any{"run_id": req.RunID, "seq": evt.Seq, "type": evt.Type, "data": evt.Data, "time": evt.Time}}); err != nil {
					return err
				}
				if evt.Type == "run_finished" {
					return s.writeRemoteWS(writeMu, conn, remoteWSMessage{Type: "run.subscribe.complete", Data: map[string]any{"run_id": req.RunID}})
				}
			}
		}
	case "run.confirm":
		if strings.TrimSpace(req.Input) == "" {
			return fmt.Errorf("input is required")
		}
		if _, ok := s.runtime.getPendingConfirmation(sessionID); !ok {
			return fmt.Errorf("no pending confirmation for session")
		}
		run, ok := s.runtime.runs.latestBySession(sessionID)
		if !ok {
			return fmt.Errorf("no run found for session")
		}
		go func(runID string) {
			_, _ = s.runtime.runAgentLoopWithRun(context.Background(), run, sessionID, UserInput{Text: req.Input}, nil, run.Mode, runnerCallbacks{callChat: s.runtime.callChatWithTools})
		}(run.ID)
		return s.writeRemoteWS(writeMu, conn, remoteWSMessage{Type: "run.confirmed", Data: map[string]any{"session_id": sessionID, "run_id": run.ID, "input": req.Input}})
	default:
		return fmt.Errorf("unsupported remote message type: %s", req.Type)
	}
}

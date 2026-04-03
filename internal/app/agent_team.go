package app

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

type agentTeamState struct {
	mu         sync.RWMutex
	ID         string
	SessionID  string
	CreatedAt  time.Time
	Tasks      map[string]teamTaskState
	Messages   []teamMessage
	SharedNote string
	Members    map[string]teamMemberState
}

type teamTaskState struct {
	ID        string    `json:"id"`
	Role      string    `json:"role"`
	Prompt    string    `json:"prompt"`
	Status    string    `json:"status"`
	Summary   string    `json:"summary,omitempty"`
	Error     string    `json:"error,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

type teamMessage struct {
	ID        string    `json:"id"`
	From      string    `json:"from"`
	To        string    `json:"to,omitempty"`
	Kind      string    `json:"kind"`
	ReplyTo   string    `json:"reply_to,omitempty"`
	ThreadID  string    `json:"thread_id,omitempty"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

type teamMemberState struct {
	ID        string    `json:"id"`
	Role      string    `json:"role"`
	SessionID string    `json:"session_id"`
	UpdatedAt time.Time `json:"updated_at"`
}

type teamContextKey struct{}
type teamSessionContextKey struct{}
type subagentDepthContextKey struct{}
type forkIsolationContextKey struct{}
type undercoverModeContextKey struct{}
type antiDistillationContextKey struct{}

func withAgentTeam(ctx context.Context, teamID string) context.Context {
	return context.WithValue(ctx, teamContextKey{}, strings.TrimSpace(teamID))
}

func withTeamSession(ctx context.Context, sessionID string) context.Context {
	return context.WithValue(ctx, teamSessionContextKey{}, strings.TrimSpace(sessionID))
}

func withSubagentDepth(ctx context.Context, depth int) context.Context {
	return context.WithValue(ctx, subagentDepthContextKey{}, depth)
}

func withForkIsolation(ctx context.Context, isolated bool) context.Context {
	return context.WithValue(ctx, forkIsolationContextKey{}, isolated)
}

func withUndercoverMode(ctx context.Context, enabled bool) context.Context {
	return context.WithValue(ctx, undercoverModeContextKey{}, enabled)
}

func undercoverModeFromContext(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	if value, ok := ctx.Value(undercoverModeContextKey{}).(bool); ok {
		return value
	}
	return false
}

func withAntiDistillation(ctx context.Context, enabled bool) context.Context {
	return context.WithValue(ctx, antiDistillationContextKey{}, enabled)
}

func antiDistillationFromContext(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	if value, ok := ctx.Value(antiDistillationContextKey{}).(bool); ok {
		return value
	}
	return false
}

func forkIsolationFromContext(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	if value, ok := ctx.Value(forkIsolationContextKey{}).(bool); ok {
		return value
	}
	return false
}

func subagentDepthFromContext(ctx context.Context) int {
	if ctx == nil {
		return 0
	}
	if value, ok := ctx.Value(subagentDepthContextKey{}).(int); ok {
		return value
	}
	return 0
}

func agentTeamIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if value, ok := ctx.Value(teamContextKey{}).(string); ok {
		return strings.TrimSpace(value)
	}
	return ""
}

func teamSessionIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if value, ok := ctx.Value(teamSessionContextKey{}).(string); ok {
		return strings.TrimSpace(value)
	}
	return ""
}

func (rt *Runtime) ensureAgentTeam(sessionID string) *agentTeamState {
	if strings.TrimSpace(sessionID) == "" {
		sessionID = "default"
	}
	if current, ok := rt.teamState.Load(sessionID); ok {
		return current.(*agentTeamState)
	}
	state := &agentTeamState{
		ID:        "team-" + uuid.NewString(),
		SessionID: sessionID,
		CreatedAt: time.Now().UTC(),
		Tasks:     map[string]teamTaskState{},
		Members:   map[string]teamMemberState{},
	}
	actual, _ := rt.teamState.LoadOrStore(sessionID, state)
	return actual.(*agentTeamState)
}

func (rt *Runtime) teamFromContext(ctx context.Context, sessionID string) *agentTeamState {
	team := rt.ensureAgentTeam(sessionID)
	teamID := agentTeamIDFromContext(ctx)
	if teamID == "" || team.ID == teamID {
		return team
	}
	return team
}

func (rt *Runtime) registerTeamTask(ctx context.Context, sessionID string, task coordinatorTask) {
	team := rt.teamFromContext(ctx, sessionID)
	team.mu.Lock()
	defer team.mu.Unlock()
	team.Tasks[task.ID] = teamTaskState{
		ID:        task.ID,
		Role:      task.Role,
		Prompt:    task.Prompt,
		Status:    "queued",
		UpdatedAt: time.Now().UTC(),
	}
	team.SharedNote = rt.renderTeamSharedContextLocked(team)
}

func (rt *Runtime) startTeamTask(ctx context.Context, sessionID string, task coordinatorTask, memberSessionID string, emit replEmitter) {
	team := rt.teamFromContext(ctx, sessionID)
	team.mu.Lock()
	defer team.mu.Unlock()
	state := team.Tasks[task.ID]
	state.Status = "running"
	state.UpdatedAt = time.Now().UTC()
	team.Tasks[task.ID] = state
	team.Members[task.ID] = teamMemberState{ID: task.ID, Role: task.Role, SessionID: memberSessionID, UpdatedAt: time.Now().UTC()}
	team.Messages = append(team.Messages, teamMessage{ID: uuid.NewString(), From: "coordinator", To: task.ID, Kind: "task_assignment", ThreadID: task.ID, Content: task.Prompt, CreatedAt: time.Now().UTC()})
	team.SharedNote = rt.renderTeamSharedContextLocked(team)
	if emit != nil {
		_ = emit("team_task_started", TeamTaskEvent{
			ID:     task.ID,
			Role:   task.Role,
			Prompt: task.Prompt,
			Status: "running",
		})
	}
}

func (rt *Runtime) finishTeamTask(ctx context.Context, sessionID string, task coordinatorTask, summary string, err error, emit replEmitter) {
	team := rt.teamFromContext(ctx, sessionID)
	team.mu.Lock()
	defer team.mu.Unlock()
	state := team.Tasks[task.ID]
	state.Status = "completed"
	status := "completed"
	if err != nil {
		state.Status = "failed"
		state.Error = err.Error()
		status = "failed"
	}
	state.Summary = strings.TrimSpace(summary)
	state.UpdatedAt = time.Now().UTC()
	team.Tasks[task.ID] = state
	content := state.Summary
	if strings.TrimSpace(content) == "" && state.Error != "" {
		content = state.Error
	}
	team.Messages = append(team.Messages, teamMessage{ID: uuid.NewString(), From: task.ID, To: "coordinator", Kind: "task_result", ThreadID: task.ID, Content: content, CreatedAt: time.Now().UTC()})
	team.SharedNote = rt.renderTeamSharedContextLocked(team)
	if emit != nil {
		eventType := "team_task_finished"
		if status == "failed" {
			eventType = "team_task_error"
		}
		_ = emit(eventType, TeamTaskEvent{
			ID:      task.ID,
			Role:    task.Role,
			Prompt:  task.Prompt,
			Status:  status,
			Summary: strings.TrimSpace(summary),
			Error:   state.Error,
		})
	}
}

func (rt *Runtime) renderTeamSharedContext(sessionID string) string {
	team := rt.ensureAgentTeam(sessionID)
	team.mu.RLock()
	defer team.mu.RUnlock()
	return strings.TrimSpace(team.SharedNote)
}

func (rt *Runtime) renderTeamSharedContextLocked(team *agentTeamState) string {
	var taskIDs []string
	for id := range team.Tasks {
		taskIDs = append(taskIDs, id)
	}
	sort.Strings(taskIDs)
	var b strings.Builder
	b.WriteString("Agent team shared context:\n")
	b.WriteString(fmt.Sprintf("- team: %s\n", team.ID))
	if len(team.Members) > 0 {
		var memberIDs []string
		for id := range team.Members {
			memberIDs = append(memberIDs, id)
		}
		sort.Strings(memberIDs)
		b.WriteString("- members:\n")
		for _, id := range memberIDs {
			member := team.Members[id]
			b.WriteString(fmt.Sprintf("  - %s | role=%s | session=%s\n", valueOrDash(member.ID), valueOrDash(member.Role), valueOrDash(member.SessionID)))
		}
	}
	b.WriteString("- tasks:\n")
	for _, id := range taskIDs {
		task := team.Tasks[id]
		line := fmt.Sprintf("  - %s [%s/%s] %s", task.ID, task.Role, task.Status, strings.TrimSpace(task.Prompt))
		if task.Summary != "" {
			line += " => " + truncate(task.Summary, 140)
		}
		if task.Error != "" {
			line += " (error: " + truncate(task.Error, 100) + ")"
		}
		b.WriteString(line + "\n")
	}
	if len(team.Messages) > 0 {
		b.WriteString("- recent_messages:\n")
		start := 0
		if len(team.Messages) > 8 {
			start = len(team.Messages) - 8
		}
		for _, msg := range team.Messages[start:] {
			thread := valueOrDash(msg.ThreadID)
			b.WriteString(fmt.Sprintf("  - %s -> %s [%s/%s] %s\n", valueOrDash(msg.From), valueOrDash(msg.To), msg.Kind, thread, truncate(msg.Content, 140)))
		}
	}
	return strings.TrimSpace(b.String())
}

func buildTeamSubagentPrompt(shared, prompt string) string {
	shared = strings.TrimSpace(shared)
	prompt = strings.TrimSpace(prompt)
	if shared == "" {
		return prompt
	}
	return shared + "\n\nYour assigned team task:\n" + prompt
}

func (rt *Runtime) sendTeamMessage(ctx context.Context, sessionID, from, to, kind, content, replyTo, threadID string, broadcast bool) (map[string]any, error) {
	team := rt.teamFromContext(ctx, sessionID)
	content = strings.TrimSpace(content)
	from = strings.TrimSpace(from)
	to = strings.TrimSpace(to)
	kind = strings.TrimSpace(kind)
	replyTo = strings.TrimSpace(replyTo)
	threadID = strings.TrimSpace(threadID)
	if content == "" {
		return nil, fmt.Errorf("content is required")
	}
	if from == "" {
		from = "agent"
	}
	if kind == "" {
		kind = "message"
	}
	team.mu.Lock()
	defer team.mu.Unlock()
	threadID = rt.resolveThreadIDLocked(team, replyTo, threadID)
	if broadcast {
		recipients := rt.teamRecipientsLocked(team)
		messageIDs := make([]string, 0, len(recipients))
		for _, recipient := range recipients {
			if strings.EqualFold(strings.TrimSpace(recipient), from) {
				continue
			}
			messageID := uuid.NewString()
			messageIDs = append(messageIDs, messageID)
			team.Messages = append(team.Messages, teamMessage{ID: messageID, From: from, To: recipient, Kind: kind, ReplyTo: replyTo, ThreadID: threadID, Content: content, CreatedAt: time.Now().UTC()})
		}
		team.SharedNote = rt.renderTeamSharedContextLocked(team)
		return map[string]any{"message_ids": messageIDs, "thread_id": threadID, "broadcast": true}, nil
	}
	if to != "" && to != "coordinator" && !rt.teamHasRecipientLocked(team, to) {
		return nil, fmt.Errorf("team recipient %s not found", to)
	}
	messageID := uuid.NewString()
	team.Messages = append(team.Messages, teamMessage{ID: messageID, From: from, To: to, Kind: kind, ReplyTo: replyTo, ThreadID: threadID, Content: content, CreatedAt: time.Now().UTC()})
	team.SharedNote = rt.renderTeamSharedContextLocked(team)
	return map[string]any{"message_id": messageID, "thread_id": threadID, "broadcast": false}, nil
}

func (rt *Runtime) resolveThreadIDLocked(team *agentTeamState, replyTo, threadID string) string {
	if threadID != "" {
		return threadID
	}
	if replyTo != "" {
		for _, msg := range team.Messages {
			if msg.ID != replyTo {
				continue
			}
			if strings.TrimSpace(msg.ThreadID) != "" {
				return strings.TrimSpace(msg.ThreadID)
			}
			return msg.ID
		}
	}
	return uuid.NewString()
}

func (rt *Runtime) teamHasRecipientLocked(team *agentTeamState, target string) bool {
	target = strings.ToLower(strings.TrimSpace(target))
	if target == "" {
		return true
	}
	for _, member := range team.Members {
		if strings.ToLower(strings.TrimSpace(member.ID)) == target || strings.ToLower(strings.TrimSpace(member.Role)) == target {
			return true
		}
	}
	return false
}

func (rt *Runtime) teamRecipientsLocked(team *agentTeamState) []string {
	seen := map[string]struct{}{}
	recipients := []string{"coordinator"}
	seen["coordinator"] = struct{}{}
	for _, member := range team.Members {
		id := strings.TrimSpace(member.ID)
		if id != "" {
			key := strings.ToLower(id)
			if _, ok := seen[key]; !ok {
				seen[key] = struct{}{}
				recipients = append(recipients, id)
			}
		}
		role := strings.TrimSpace(member.Role)
		if role != "" {
			key := strings.ToLower(role)
			if _, ok := seen[key]; !ok {
				seen[key] = struct{}{}
				recipients = append(recipients, role)
			}
		}
	}
	sort.Strings(recipients[1:])
	return recipients
}

func (rt *Runtime) formatTeamStatus(sessionID string) string {
	team := rt.ensureAgentTeam(sessionID)
	team.mu.RLock()
	defer team.mu.RUnlock()
	return rt.renderTeamSharedContextLocked(team)
}

func (rt *Runtime) formatTeamTasks(sessionID string) string {
	team := rt.ensureAgentTeam(sessionID)
	team.mu.RLock()
	defer team.mu.RUnlock()
	if len(team.Tasks) == 0 {
		return "No team tasks available."
	}
	var ids []string
	for id := range team.Tasks {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var b strings.Builder
	b.WriteString("Team tasks:\n")
	for _, id := range ids {
		task := team.Tasks[id]
		b.WriteString(fmt.Sprintf("- %s | role=%s | status=%s | %s\n", task.ID, valueOrDash(task.Role), valueOrDash(task.Status), strings.TrimSpace(task.Prompt)))
		if task.Summary != "" {
			b.WriteString(fmt.Sprintf("  summary: %s\n", truncate(task.Summary, 200)))
		}
		if task.Error != "" {
			b.WriteString(fmt.Sprintf("  error: %s\n", truncate(task.Error, 200)))
		}
	}
	return strings.TrimSpace(b.String())
}

func (rt *Runtime) formatTeamMessages(sessionID, threadID string) string {
	team := rt.ensureAgentTeam(sessionID)
	team.mu.RLock()
	defer team.mu.RUnlock()
	threadID = strings.TrimSpace(threadID)
	var filtered []teamMessage
	for _, msg := range team.Messages {
		if threadID != "" && strings.TrimSpace(msg.ThreadID) != threadID {
			continue
		}
		filtered = append(filtered, msg)
	}
	if len(filtered) == 0 {
		if threadID != "" {
			return fmt.Sprintf("No team messages found for thread %s.", threadID)
		}
		return "No team messages available."
	}
	start := 0
	if len(filtered) > 20 {
		start = len(filtered) - 20
	}
	var b strings.Builder
	b.WriteString("Team messages:\n")
	if threadID != "" {
		b.WriteString(fmt.Sprintf("- thread: %s\n", threadID))
	}
	for _, msg := range filtered[start:] {
		b.WriteString(fmt.Sprintf("- %s | id=%s | %s -> %s | %s | thread=%s | reply_to=%s | %s\n", msg.CreatedAt.Format(time.RFC3339), valueOrDash(msg.ID), valueOrDash(msg.From), valueOrDash(msg.To), valueOrDash(msg.Kind), valueOrDash(msg.ThreadID), valueOrDash(msg.ReplyTo), truncate(msg.Content, 200)))
	}
	return strings.TrimSpace(b.String())
}

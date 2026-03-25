package convo

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/zetatez/morpheus/pkg/sdk"
)

const (
	MaxMessagesBeforeSummary = 32
	RecentMessagesToKeep     = 32
)

// Session holds messages and optional summary for a session.
type Session struct {
	Messages []sdk.Message
	Summary  string
	ParentID string
	ForkID   string
}

// Manager maintains lightweight in-memory conversation state.
type Manager struct {
	mu           sync.RWMutex
	sessions     map[string]*Session
	systemPrompt string
}

// NewManager builds a new conversation manager instance.
func NewManager() *Manager {
	return &Manager{sessions: make(map[string]*Session)}
}

// SetSystemPrompt stores a pinned system prompt used for all sessions.
func (m *Manager) SetSystemPrompt(prompt string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.systemPrompt = strings.TrimSpace(prompt)
}

// SystemPrompt returns the pinned system prompt.
func (m *Manager) SystemPrompt() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.systemPrompt
}

// Append stores a message for the provided session.
func (m *Manager) Append(ctx context.Context, sessionID, role, content string) (sdk.Message, error) {
	return m.AppendWithParts(ctx, sessionID, role, content, nil)
}

// AppendWithParts stores a message with structured parts for the provided session.
func (m *Manager) AppendWithParts(ctx context.Context, sessionID, role, content string, parts []sdk.MessagePart) (sdk.Message, error) {
	msg := sdk.Message{
		ID:        uuid.NewString(),
		Role:      role,
		Content:   content,
		Parts:     parts,
		Timestamp: time.Now(),
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.sessions[sessionID] == nil {
		m.sessions[sessionID] = &Session{}
	}

	// Trim only if we already have a summary to preserve context quality.
	if len(m.sessions[sessionID].Messages) >= MaxMessagesBeforeSummary {
		m.trimSession(sessionID)
	}

	m.sessions[sessionID].Messages = append(m.sessions[sessionID].Messages, msg)
	return msg, nil
}

// trimSession keeps only recent messages if a summary exists.
func (m *Manager) trimSession(sessionID string) {
	sess := m.sessions[sessionID]
	if sess == nil || len(sess.Messages) < RecentMessagesToKeep {
		return
	}
	if strings.TrimSpace(sess.Summary) == "" {
		return
	}
	sess.Messages = sess.Messages[len(sess.Messages)-RecentMessagesToKeep:]
}

// Messages returns the raw messages for a session (no system/summary injected).
func (m *Manager) Messages(sessionID string) []sdk.Message {
	m.mu.RLock()
	defer m.mu.RUnlock()
	sess := m.sessions[sessionID]
	if sess == nil {
		return nil
	}
	msgs := make([]sdk.Message, len(sess.Messages))
	copy(msgs, sess.Messages)
	return msgs
}

// Summary returns the stored summary for a session.
func (m *Manager) Summary(sessionID string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	sess := m.sessions[sessionID]
	if sess == nil {
		return ""
	}
	return sess.Summary
}

// SetSummary stores the summary for a session.
func (m *Manager) SetSummary(sessionID, summary string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sessions[sessionID] == nil {
		m.sessions[sessionID] = &Session{}
	}
	m.sessions[sessionID].Summary = strings.TrimSpace(summary)
	if len(m.sessions[sessionID].Messages) >= RecentMessagesToKeep {
		m.sessions[sessionID].Messages = m.sessions[sessionID].Messages[len(m.sessions[sessionID].Messages)-RecentMessagesToKeep:]
	}
}

// ReplaceMessages replaces the session messages with the provided slice.
func (m *Manager) ReplaceMessages(sessionID string, messages []sdk.Message) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sessions[sessionID] == nil {
		m.sessions[sessionID] = &Session{}
	}
	cloned := make([]sdk.Message, len(messages))
	copy(cloned, messages)
	m.sessions[sessionID].Messages = cloned
}

// History returns messages for the session.
func (m *Manager) History(ctx context.Context, sessionID string) []sdk.Message {
	m.mu.RLock()
	defer m.mu.RUnlock()

	sess := m.sessions[sessionID]
	if sess == nil {
		if m.systemPrompt == "" {
			return nil
		}
		return []sdk.Message{{
			ID:      uuid.NewString(),
			Role:    "system",
			Content: m.systemPrompt,
		}}
	}

	result := make([]sdk.Message, 0, len(sess.Messages)+2)
	if m.systemPrompt != "" {
		result = append(result, sdk.Message{
			ID:      uuid.NewString(),
			Role:    "system",
			Content: m.systemPrompt,
		})
	}

	// Prepend summary if exists
	if sess.Summary != "" {
		summaryMsg := sdk.Message{
			ID:      uuid.NewString(),
			Role:    "system",
			Content: sess.Summary,
		}
		result = append(result, summaryMsg)
	}
	for _, msg := range sess.Messages {
		if m.systemPrompt != "" && msg.Role == "system" && msg.Content == m.systemPrompt {
			continue
		}
		result = append(result, msg)
	}
	return result
}

// Clear removes all messages for a session.
func (m *Manager) Clear(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, sessionID)
}

// ForkSession creates a new session forked from an existing session.
// The fork starts with the same messages as the parent but is independent.
func (m *Manager) ForkSession(parentID, forkID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	parent := m.sessions[parentID]
	if parent == nil {
		return fmt.Errorf("parent session %s not found", parentID)
	}

	fork := &Session{
		Messages: make([]sdk.Message, len(parent.Messages)),
		Summary:  parent.Summary,
		ParentID: parentID,
		ForkID:   forkID,
	}
	copy(fork.Messages, parent.Messages)
	m.sessions[forkID] = fork
	return nil
}

// GetParentID returns the parent session ID if this session is a fork.
func (m *Manager) GetParentID(sessionID string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	sess := m.sessions[sessionID]
	if sess == nil {
		return ""
	}
	return sess.ParentID
}

// IsFork returns true if this session is a fork of another session.
func (m *Manager) IsFork(sessionID string) bool {
	return m.GetParentID(sessionID) != ""
}

// GetForks returns all session IDs that are forks of the given parent.
func (m *Manager) GetForks(parentID string) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var forks []string
	for id, sess := range m.sessions {
		if sess.ParentID == parentID {
			forks = append(forks, id)
		}
	}
	return forks
}

// ForkFromMessage creates a fork starting from a specific message index.
// All messages up to (but not including) startIndex are copied.
func (m *Manager) ForkFromMessage(parentID, forkID string, startIndex int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	parent := m.sessions[parentID]
	if parent == nil {
		return fmt.Errorf("parent session %s not found", parentID)
	}

	if startIndex < 0 || startIndex > len(parent.Messages) {
		startIndex = 0
	}

	fork := &Session{
		Messages: make([]sdk.Message, len(parent.Messages[startIndex:])),
		Summary:  parent.Summary,
		ParentID: parentID,
		ForkID:   forkID,
	}
	copy(fork.Messages, parent.Messages[startIndex:])
	m.sessions[forkID] = fork
	return nil
}

// Summaries returns naive markdown summaries when history grows large.
func (m *Manager) Summaries(sessionID string) []sdk.SummaryChunk {
	m.mu.RLock()
	sess := m.sessions[sessionID]
	var summary string
	var msgs []sdk.Message
	if sess != nil {
		summary = sess.Summary
		msgs = sess.Messages
	}
	m.mu.RUnlock()

	if summary == "" && len(msgs) == 0 {
		return nil
	}

	var chunks []sdk.SummaryChunk
	if strings.TrimSpace(summary) != "" {
		chunks = append(chunks, sdk.SummaryChunk{
			ID:      uuid.NewString(),
			Scope:   "summary",
			Content: summary,
		})
	}
	if len(msgs) > 0 {
		recent := msgs
		if len(recent) > 8 {
			recent = recent[len(recent)-8:]
		}
		var ids []string
		for _, msg := range recent {
			if msg.ID != "" {
				ids = append(ids, msg.ID)
			}
		}
		chunks = append(chunks, sdk.SummaryChunk{
			ID:               uuid.NewString(),
			Scope:            "recent",
			Content:          toBulletSummary(recent),
			SourceMessageIDs: ids,
		})
	}
	return chunks
}

func toBulletSummary(msgs []sdk.Message) string {
	var b strings.Builder
	for _, msg := range msgs {
		b.WriteString("- ")
		b.WriteString(msg.Role)
		b.WriteString(": ")
		snippet := msg.Content
		if len(snippet) > 120 {
			snippet = snippet[:120] + "..."
		}
		b.WriteString(snippet)
		b.WriteString("\n")
	}
	return b.String()
}

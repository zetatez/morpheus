package convo

import (
	"context"
	"testing"
	"time"

	"github.com/zetatez/morpheus/pkg/sdk"
)

func TestNewManager(t *testing.T) {
	m := NewManager()
	if m == nil {
		t.Fatal("NewManager returned nil")
	}
	if m.sessions == nil {
		t.Error("sessions map is nil")
	}
}

func TestSetSystemPrompt(t *testing.T) {
	m := NewManager()

	m.SetSystemPrompt("  test prompt  ")
	if got := m.SystemPrompt(); got != "test prompt" {
		t.Errorf("SystemPrompt() = %q, want %q", got, "test prompt")
	}

	m.SetSystemPrompt("")
	if got := m.SystemPrompt(); got != "" {
		t.Errorf("SystemPrompt() = %q, want empty", got)
	}
}

func TestAppend(t *testing.T) {
	m := NewManager()

	msg, err := m.Append(context.Background(), "session1", "user", "hello")
	if err != nil {
		t.Errorf("Append() error = %v", err)
	}
	if msg.Role != "user" {
		t.Errorf("msg.Role = %q, want %q", msg.Role, "user")
	}
	if msg.Content != "hello" {
		t.Errorf("msg.Content = %q, want %q", msg.Content, "hello")
	}
	if msg.ID == "" {
		t.Error("msg.ID should not be empty")
	}
}

func TestAppendWithParts(t *testing.T) {
	m := NewManager()

	parts := []sdk.MessagePart{
		{Type: "text", Text: "part1"},
	}
	msg, err := m.AppendWithParts(context.Background(), "session1", "assistant", "response", parts)
	if err != nil {
		t.Errorf("AppendWithParts() error = %v", err)
	}
	if len(msg.Parts) != 1 {
		t.Errorf("len(msg.Parts) = %d, want 1", len(msg.Parts))
	}
	if msg.Parts[0].Text != "part1" {
		t.Errorf("msg.Parts[0].Text = %q, want %q", msg.Parts[0].Text, "part1")
	}
}

func TestHistory(t *testing.T) {
	m := NewManager()

	_, _ = m.Append(context.Background(), "session1", "user", "hello")
	_, _ = m.Append(context.Background(), "session1", "assistant", "hi")

	msgs := m.History(context.Background(), "session1")
	if len(msgs) != 2 {
		t.Errorf("len(msgs) = %d, want 2", len(msgs))
	}
}

func TestHistoryEmpty(t *testing.T) {
	m := NewManager()

	msgs := m.History(context.Background(), "nonexistent")
	if msgs != nil && len(msgs) != 0 {
		t.Errorf("len(msgs) = %d, want 0 or nil", len(msgs))
	}
}

func TestHistoryWithSystemPrompt(t *testing.T) {
	m := NewManager()
	m.SetSystemPrompt("You are a helpful assistant")

	_, _ = m.Append(context.Background(), "session1", "user", "hello")

	msgs := m.History(context.Background(), "session1")
	if len(msgs) < 1 {
		t.Error("History should return at least the system prompt")
	}
	if msgs[0].Role != "system" {
		t.Errorf("first message role = %q, want %q", msgs[0].Role, "system")
	}
}

func TestMessages(t *testing.T) {
	m := NewManager()

	_, _ = m.Append(context.Background(), "session1", "user", "hello")

	msgs := m.Messages("session1")
	if len(msgs) != 1 {
		t.Errorf("len(msgs) = %d, want 1", len(msgs))
	}
	if msgs[0].Content != "hello" {
		t.Errorf("msgs[0].Content = %q, want %q", msgs[0].Content, "hello")
	}
}

func TestMessagesNil(t *testing.T) {
	m := NewManager()

	msgs := m.Messages("nonexistent")
	if msgs != nil {
		t.Errorf("Messages() = %v, want nil", msgs)
	}
}

func TestSummary(t *testing.T) {
	m := NewManager()

	_, _ = m.Append(context.Background(), "session1", "user", "hello")

	if got := m.Summary("session1"); got != "" {
		t.Errorf("Summary() = %q, want empty", got)
	}

	m.SetSummary("session1", "  summarized content  ")

	if got := m.Summary("session1"); got != "summarized content" {
		t.Errorf("Summary() = %q, want %q", got, "summarized content")
	}
}

func TestSetSummary(t *testing.T) {
	m := NewManager()

	m.SetSummary("session1", "test summary")

	if got := m.Summary("session1"); got != "test summary" {
		t.Errorf("Summary() = %q, want %q", got, "test summary")
	}
}

func TestReplaceMessages(t *testing.T) {
	m := NewManager()

	_, _ = m.Append(context.Background(), "session1", "user", "original")

	newMsgs := []sdk.Message{
		{ID: "new1", Role: "user", Content: "replaced"},
	}
	m.ReplaceMessages("session1", newMsgs)

	msgs := m.Messages("session1")
	if len(msgs) != 1 {
		t.Errorf("len(msgs) = %d, want 1", len(msgs))
	}
	if msgs[0].Content != "replaced" {
		t.Errorf("msgs[0].Content = %q, want %q", msgs[0].Content, "replaced")
	}
}

func TestMessageTimestamp(t *testing.T) {
	m := NewManager()

	before := time.Now()
	_, _ = m.Append(context.Background(), "session1", "user", "hello")
	after := time.Now()

	msgs := m.Messages("session1")
	if len(msgs) == 0 {
		t.Fatal("no messages returned")
	}
	if msgs[0].Timestamp.Before(before) || msgs[0].Timestamp.After(after) {
		t.Error("message timestamp out of expected range")
	}
}

func TestMultipleSessions(t *testing.T) {
	m := NewManager()

	_, _ = m.Append(context.Background(), "session1", "user", "hello")
	_, _ = m.Append(context.Background(), "session2", "user", "world")
	_, _ = m.Append(context.Background(), "session3", "assistant", "hi")

	msgs1 := m.Messages("session1")
	msgs2 := m.Messages("session2")
	msgs3 := m.Messages("session3")

	if len(msgs1) != 1 || len(msgs2) != 1 || len(msgs3) != 1 {
		t.Error("each session should have 1 message")
	}
}

package session

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zetatez/morpheus/pkg/sdk"
)

type Writer struct {
	sessionPath string
	retention   time.Duration
}

const conversationFilename = "conversation.raw.md"
const metadataFilename = "session.meta.json"

type Metadata struct {
	SessionID        string    `json:"session_id"`
	UpdatedAt        time.Time `json:"updated_at"`
	Summary          string    `json:"summary,omitempty"`
	ShortTerm        string    `json:"short_term,omitempty"`
	LongTerm         string    `json:"long_term,omitempty"`
	AllowedSkills    []string  `json:"allowed_skills,omitempty"`
	AllowedSubagents []string  `json:"allowed_subagents,omitempty"`
	LastTaskNote     string    `json:"last_task_note,omitempty"`
	CompressedAt     time.Time `json:"compressed_at,omitempty"`
	IsCodeTask       bool      `json:"is_code_task,omitempty"`
}

func NewWriter(sessionPath string, retention time.Duration) *Writer {
	if sessionPath == "" {
		sessionPath = defaultSessionPath()
	}
	return &Writer{
		sessionPath: sessionPath,
		retention:   retention,
	}
}

func defaultSessionPath() string {
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".local", "share", "morpheus", "sessions")
	}
	return "./data/sessions"
}

func (w *Writer) Write(ctx context.Context, sessionID string, messages []sdk.Message) error {
	if w.sessionPath == "" {
		return nil
	}

	sessionDir := filepath.Join(w.sessionPath, sessionID)
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		return err
	}

	return w.writeTranscript(sessionDir, messages)
}

func (w *Writer) writeTranscript(sessionDir string, messages []sdk.Message) error {
	filename := filepath.Join(sessionDir, conversationFilename)

	// Read existing messages count to avoid duplication
	var existingCount int
	if data, err := os.ReadFile(filename); err == nil {
		existingCount = strings.Count(string(data), "## ")
	}

	// Only append new messages
	if len(messages) <= existingCount {
		return nil
	}

	f, err := os.OpenFile(filename, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	for i := existingCount; i < len(messages); i++ {
		msg := messages[i]
		timestamp := msg.Timestamp.Format(time.RFC3339)
		role := msg.Role
		if role == "" {
			role = "user"
		}
		content := renderMessageContent(msg)
		fmt.Fprintf(f, "## %s | %s\n\n%s\n\n---\n\n", role, timestamp, content)
	}
	return nil
}

func renderMessageContent(msg sdk.Message) string {
	if len(msg.Parts) == 0 {
		return msg.Content
	}
	var b strings.Builder
	if strings.TrimSpace(msg.Content) != "" {
		b.WriteString(msg.Content)
		b.WriteString("\n\n")
	}
	for _, part := range msg.Parts {
		switch part.Type {
		case "text":
			if part.Text != "" {
				b.WriteString(part.Text)
				b.WriteString("\n")
			}
		case "tool":
			status := part.Status
			if status == "" {
				if part.Error != "" {
					status = "error"
				} else {
					status = "completed"
				}
			}
			if part.CallID != "" {
				b.WriteString(fmt.Sprintf("### Tool: %s (%s, call_id=%s)\n", part.Tool, status, part.CallID))
			} else {
				b.WriteString(fmt.Sprintf("### Tool: %s (%s)\n", part.Tool, status))
			}
			if len(part.Input) > 0 {
				b.WriteString("Input:\n")
				b.WriteString(truncateString(renderJSON(part.Input), 2000))
				b.WriteString("\n")
			}
			if len(part.Output) > 0 {
				b.WriteString("Output:\n")
				b.WriteString(truncateString(renderJSON(part.Output), 4000))
				b.WriteString("\n")
			}
			if part.Error != "" {
				b.WriteString("Error: ")
				b.WriteString(part.Error)
				b.WriteString("\n")
			}
			b.WriteString("\n")
		case "attachment":
			b.WriteString("### Attachment\n")
			if part.Input != nil {
				if name, ok := part.Input["name"].(string); ok && strings.TrimSpace(name) != "" {
					b.WriteString("Name: ")
					b.WriteString(name)
					b.WriteString("\n")
				}
				if kind, ok := part.Input["kind"].(string); ok && strings.TrimSpace(kind) != "" {
					b.WriteString("Kind: ")
					b.WriteString(kind)
					b.WriteString("\n")
				}
				if path, ok := part.Input["path"].(string); ok && strings.TrimSpace(path) != "" {
					b.WriteString("Path: ")
					b.WriteString(path)
					b.WriteString("\n")
				}
				if url, ok := part.Input["url"].(string); ok && strings.TrimSpace(url) != "" {
					b.WriteString("URL: ")
					b.WriteString(url)
					b.WriteString("\n")
				}
			}
			b.WriteString("\n")
		}
	}
	return strings.TrimSpace(b.String())
}

func renderJSON(value any) string {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Sprintf("%v", value)
	}
	return string(data)
}

func truncateString(value string, max int) string {
	if max <= 0 || len(value) <= max {
		return value
	}
	return value[:max] + "..."
}

func (w *Writer) SaveSummary(ctx context.Context, sessionID, content string) error {
	return w.saveMemoryFile(ctx, sessionID, "summary.md", content)
}

func (w *Writer) SaveShortTerm(ctx context.Context, sessionID, content string) error {
	return w.saveMemoryFile(ctx, sessionID, "short_term.md", content)
}

func (w *Writer) SaveLongTerm(ctx context.Context, sessionID, content string) error {
	return w.saveMemoryFile(ctx, sessionID, "long_term.md", content)
}

func (w *Writer) SaveMetadata(ctx context.Context, sessionID string, meta Metadata) error {
	if w.sessionPath == "" {
		return nil
	}
	sessionDir := filepath.Join(w.sessionPath, sessionID)
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		return err
	}
	meta.SessionID = sessionID
	meta.UpdatedAt = time.Now().UTC()
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(sessionDir, metadataFilename), data, 0o644)
}

func (w *Writer) SaveToolOutput(ctx context.Context, sessionID, toolName string, content []byte) (string, error) {
	if w.sessionPath == "" {
		return "", nil
	}
	if toolName == "" {
		toolName = "tool"
	}
	toolName = strings.ReplaceAll(toolName, "/", "-")
	toolName = strings.ReplaceAll(toolName, " ", "-")

	sessionDir := filepath.Join(w.sessionPath, sessionID)
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		return "", err
	}
	filename := fmt.Sprintf("tool-output-%s-%s.txt", time.Now().Format("20060102-150405"), toolName)
	path := filepath.Join(sessionDir, filename)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

func (w *Writer) saveMemoryFile(ctx context.Context, sessionID, filename, content string) error {
	if w.sessionPath == "" {
		return nil
	}

	sessionDir := filepath.Join(w.sessionPath, sessionID)
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		return err
	}

	filepath := filepath.Join(sessionDir, filename)
	return os.WriteFile(filepath, []byte(content), 0o644)
}

func (w *Writer) LoadSession(ctx context.Context, sessionID string) (Session, error) {
	session := Session{ID: sessionID}

	if w.sessionPath == "" {
		return session, nil
	}

	sessionDir := filepath.Join(w.sessionPath, sessionID)

	conversation, _ := os.ReadFile(filepath.Join(sessionDir, conversationFilename))
	session.Conversation = string(conversation)

	shortTerm, _ := os.ReadFile(filepath.Join(sessionDir, "short_term.md"))
	session.ShortTermMemory = string(shortTerm)

	longTerm, _ := os.ReadFile(filepath.Join(sessionDir, "long_term.md"))
	session.LongTermMemory = string(longTerm)

	summary, _ := os.ReadFile(filepath.Join(sessionDir, "summary.md"))
	session.Summary = string(summary)

	metadata, _ := os.ReadFile(filepath.Join(sessionDir, metadataFilename))
	if len(metadata) > 0 {
		_ = json.Unmarshal(metadata, &session.Metadata)
	}

	return session, nil
}

type Session struct {
	ID              string
	Conversation    string
	ShortTermMemory string
	LongTermMemory  string
	Summary         string
	Metadata        Metadata
}

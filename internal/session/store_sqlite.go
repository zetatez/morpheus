package session

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/zetatez/morpheus/pkg/sdk"
	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

type StoredSession struct {
	ID        string
	CreatedAt time.Time
	UpdatedAt time.Time
	Summary   string
	Metadata  Metadata
	Messages  []sdk.Message
}

func NewStore(path string) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	store := &Store{db: db}
	if err := store.ensureSchema(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) ensureSchema(ctx context.Context) error {
	stmts := []string{
		`PRAGMA foreign_keys = ON;`,
		`CREATE TABLE IF NOT EXISTS sessions (
			id TEXT PRIMARY KEY,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			summary TEXT,
			metadata_json TEXT
		);`,
		`CREATE TABLE IF NOT EXISTS messages (
			id TEXT PRIMARY KEY,
			session_id TEXT NOT NULL,
			idx INTEGER NOT NULL,
			role TEXT,
			content TEXT,
			parts_json TEXT,
			timestamp TEXT,
			FOREIGN KEY(session_id) REFERENCES sessions(id) ON DELETE CASCADE
		);`,
		`CREATE INDEX IF NOT EXISTS idx_messages_session ON messages(session_id, idx);`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_updated ON sessions(updated_at);`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) SaveSession(ctx context.Context, sessionID string, messages []sdk.Message, summary string, meta Metadata) error {
	if s == nil || s.db == nil {
		return nil
	}
	if strings.TrimSpace(sessionID) == "" {
		return nil
	}
	meta.SessionID = sessionID
	meta.UpdatedAt = time.Now().UTC()
	metaJSON, _ := json.Marshal(meta)

	createdAt := time.Now().UTC()
	updatedAt := meta.UpdatedAt

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	_, err = tx.ExecContext(ctx,
		`INSERT OR IGNORE INTO sessions (id, created_at, updated_at, summary, metadata_json) VALUES (?, ?, ?, ?, ?)`,
		sessionID,
		createdAt.Format(time.RFC3339Nano),
		updatedAt.Format(time.RFC3339Nano),
		summary,
		string(metaJSON),
	)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE sessions SET updated_at = ?, summary = ?, metadata_json = ? WHERE id = ?`,
		updatedAt.Format(time.RFC3339Nano), summary, string(metaJSON), sessionID)
	if err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx, `DELETE FROM messages WHERE session_id = ?`, sessionID)
	if err != nil {
		return err
	}

	stmt, err := tx.PrepareContext(ctx, `INSERT INTO messages (id, session_id, idx, role, content, parts_json, timestamp) VALUES (?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for idx, msg := range messages {
		id := msg.ID
		if strings.TrimSpace(id) == "" {
			id = uuid.NewString()
		}
		partsJSON := ""
		if len(msg.Parts) > 0 {
			if data, err := json.Marshal(msg.Parts); err == nil {
				partsJSON = string(data)
			}
		}
		ts := msg.Timestamp
		if ts.IsZero() {
			ts = time.Now().UTC()
		}
		_, err = stmt.ExecContext(ctx, id, sessionID, idx, msg.Role, msg.Content, partsJSON, ts.Format(time.RFC3339Nano))
		if err != nil {
			return err
		}
	}

	if err = tx.Commit(); err != nil {
		return err
	}
	return nil
}

func (s *Store) ListSessions(ctx context.Context, query string) ([]Metadata, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	query = strings.TrimSpace(query)
	var rows *sql.Rows
	var err error
	if query == "" {
		rows, err = s.db.QueryContext(ctx, `SELECT id, created_at, updated_at, summary, metadata_json FROM sessions ORDER BY updated_at DESC LIMIT 200`)
	} else {
		like := "%" + strings.ToLower(query) + "%"
		rows, err = s.db.QueryContext(ctx, `SELECT id, created_at, updated_at, summary, metadata_json FROM sessions WHERE LOWER(id) LIKE ? OR LOWER(summary) LIKE ? OR LOWER(metadata_json) LIKE ? ORDER BY updated_at DESC LIMIT 200`, like, like, like)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Metadata
	for rows.Next() {
		var id, createdAtRaw, updatedAtRaw, summary, metaJSON string
		if err := rows.Scan(&id, &createdAtRaw, &updatedAtRaw, &summary, &metaJSON); err != nil {
			continue
		}
		var meta Metadata
		if metaJSON != "" {
			_ = json.Unmarshal([]byte(metaJSON), &meta)
		}
		meta.SessionID = id
		if summary != "" && meta.Summary == "" {
			meta.Summary = summary
		}
		if updatedAtRaw != "" {
			if t, err := time.Parse(time.RFC3339Nano, updatedAtRaw); err == nil {
				meta.UpdatedAt = t
			}
		}
		if createdAtRaw != "" && meta.UpdatedAt.IsZero() {
			if t, err := time.Parse(time.RFC3339Nano, createdAtRaw); err == nil {
				meta.UpdatedAt = t
			}
		}
		out = append(out, meta)
	}
	return out, nil
}

func (s *Store) LoadSession(ctx context.Context, sessionID string) (StoredSession, error) {
	var out StoredSession
	if s == nil || s.db == nil {
		return out, sql.ErrNoRows
	}
	row := s.db.QueryRowContext(ctx, `SELECT created_at, updated_at, summary, metadata_json FROM sessions WHERE id = ?`, sessionID)
	var createdAtRaw, updatedAtRaw, summary, metaJSON string
	if err := row.Scan(&createdAtRaw, &updatedAtRaw, &summary, &metaJSON); err != nil {
		return out, err
	}
	var meta Metadata
	if metaJSON != "" {
		_ = json.Unmarshal([]byte(metaJSON), &meta)
	}
	meta.SessionID = sessionID
	meta.Summary = summary

	rows, err := s.db.QueryContext(ctx, `SELECT id, role, content, parts_json, timestamp FROM messages WHERE session_id = ? ORDER BY idx ASC`, sessionID)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	var messages []sdk.Message
	for rows.Next() {
		var id, role, content, partsJSON, tsRaw string
		if err := rows.Scan(&id, &role, &content, &partsJSON, &tsRaw); err != nil {
			continue
		}
		msg := sdk.Message{ID: id, Role: role, Content: content}
		if partsJSON != "" {
			_ = json.Unmarshal([]byte(partsJSON), &msg.Parts)
		}
		if tsRaw != "" {
			if t, err := time.Parse(time.RFC3339Nano, tsRaw); err == nil {
				msg.Timestamp = t
			}
		}
		messages = append(messages, msg)
	}
	createdAt, _ := time.Parse(time.RFC3339Nano, createdAtRaw)
	updatedAt, _ := time.Parse(time.RFC3339Nano, updatedAtRaw)
	out = StoredSession{ID: sessionID, CreatedAt: createdAt, UpdatedAt: updatedAt, Summary: summary, Metadata: meta, Messages: messages}
	return out, nil
}

func (s *Store) ConversationMarkdown(sessionID string, messages []sdk.Message) string {
	if len(messages) == 0 {
		return ""
	}
	var b strings.Builder
	for _, msg := range messages {
		role := msg.Role
		if role == "" {
			role = "user"
		}
		ts := msg.Timestamp
		if ts.IsZero() {
			ts = time.Now().UTC()
		}
		content := renderMessageContent(msg)
		b.WriteString(fmt.Sprintf("## %s | %s\n\n%s\n\n---\n\n", role, ts.Format(time.RFC3339), content))
	}
	return strings.TrimSpace(b.String())
}

func (s *Store) DeleteSession(ctx context.Context, sessionID string) error {
	if s == nil || s.db == nil {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, sessionID)
	return err
}

func (s *Store) HasSession(ctx context.Context, sessionID string) bool {
	if s == nil || s.db == nil {
		return false
	}
	row := s.db.QueryRowContext(ctx, `SELECT 1 FROM sessions WHERE id = ?`, sessionID)
	var v int
	if err := row.Scan(&v); err != nil {
		return false
	}
	return true
}

func IsNotFound(err error) bool {
	return errors.Is(err, sql.ErrNoRows)
}

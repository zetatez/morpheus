package session

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

type StoredRun struct {
	ID        string
	SessionID string
	Status    string
	Reply     string
	Error     string
	LastStep  string
	CreatedAt time.Time
	UpdatedAt time.Time
	Events    []StoredRunEvent
}

type StoredRunEvent struct {
	Seq  int64
	Type string
	Data string
	Time time.Time
}

func (s *Store) EnsureRunSchema(ctx context.Context) error {
	if s == nil || s.db == nil {
		return nil
	}
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS runs (
			id TEXT PRIMARY KEY,
			session_id TEXT NOT NULL,
			status TEXT NOT NULL,
			reply TEXT,
			error TEXT,
			last_step TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS run_events (
			run_id TEXT NOT NULL,
			seq INTEGER NOT NULL,
			type TEXT NOT NULL,
			data_json TEXT,
			time TEXT NOT NULL,
			PRIMARY KEY(run_id, seq),
			FOREIGN KEY(run_id) REFERENCES runs(id) ON DELETE CASCADE
		);`,
		`CREATE INDEX IF NOT EXISTS idx_runs_session_updated ON runs(session_id, updated_at DESC);`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) SaveRun(ctx context.Context, run StoredRun) error {
	if s == nil || s.db == nil {
		return nil
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO runs (id, session_id, status, reply, error, last_step, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET status=excluded.status, reply=excluded.reply, error=excluded.error, last_step=excluded.last_step, updated_at=excluded.updated_at`,
		run.ID, run.SessionID, run.Status, run.Reply, run.Error, run.LastStep,
		run.CreatedAt.Format(time.RFC3339Nano), run.UpdatedAt.Format(time.RFC3339Nano),
	)
	return err
}

func (s *Store) AppendRunEvent(ctx context.Context, runID string, seq int64, eventType string, data map[string]any, ts time.Time) error {
	if s == nil || s.db == nil {
		return nil
	}
	dataJSON := ""
	if data != nil {
		b, _ := json.Marshal(data)
		dataJSON = string(b)
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO run_events (run_id, seq, type, data_json, time) VALUES (?, ?, ?, ?, ?)`,
		runID, seq, eventType, dataJSON, ts.Format(time.RFC3339Nano),
	)
	return err
}

func (s *Store) LoadLatestRunBySession(ctx context.Context, sessionID string) (StoredRun, error) {
	var out StoredRun
	if s == nil || s.db == nil {
		return out, sql.ErrNoRows
	}
	row := s.db.QueryRowContext(ctx, `SELECT id, session_id, status, reply, error, last_step, created_at, updated_at FROM runs WHERE session_id = ? ORDER BY updated_at DESC LIMIT 1`, sessionID)
	var createdAtRaw, updatedAtRaw string
	if err := row.Scan(&out.ID, &out.SessionID, &out.Status, &out.Reply, &out.Error, &out.LastStep, &createdAtRaw, &updatedAtRaw); err != nil {
		return out, err
	}
	out.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAtRaw)
	out.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAtRaw)
	rows, err := s.db.QueryContext(ctx, `SELECT seq, type, data_json, time FROM run_events WHERE run_id = ? ORDER BY seq ASC`, out.ID)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var evt StoredRunEvent
		var timeRaw string
		if err := rows.Scan(&evt.Seq, &evt.Type, &evt.Data, &timeRaw); err != nil {
			continue
		}
		evt.Time, _ = time.Parse(time.RFC3339Nano, timeRaw)
		out.Events = append(out.Events, evt)
	}
	return out, nil
}

func (s *Store) RecoverUnfinishedRuns(ctx context.Context) ([]StoredRun, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, session_id, status, reply, error, last_step, created_at, updated_at FROM runs WHERE status IN ('queued','running','waiting_tool','replaying')`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StoredRun
	for rows.Next() {
		var run StoredRun
		var createdAtRaw, updatedAtRaw string
		if err := rows.Scan(&run.ID, &run.SessionID, &run.Status, &run.Reply, &run.Error, &run.LastStep, &createdAtRaw, &updatedAtRaw); err != nil {
			continue
		}
		run.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAtRaw)
		run.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAtRaw)
		out = append(out, run)
	}
	return out, nil
}

func (s *Store) ListRunsBySession(ctx context.Context, sessionID string, status string, limit int, cursor string) ([]StoredRun, string, error) {
	if s == nil || s.db == nil {
		return nil, "", nil
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	query := `SELECT id, session_id, status, reply, error, last_step, created_at, updated_at FROM runs WHERE session_id = ?`
	args := []any{sessionID}
	if status != "" {
		query += ` AND status = ?`
		args = append(args, status)
	}
	if cursor != "" {
		query += ` AND updated_at < ?`
		args = append(args, cursor)
	}
	query += ` ORDER BY updated_at DESC LIMIT ?`
	args = append(args, limit+1)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	var out []StoredRun
	for rows.Next() {
		var run StoredRun
		var createdAtRaw, updatedAtRaw string
		if err := rows.Scan(&run.ID, &run.SessionID, &run.Status, &run.Reply, &run.Error, &run.LastStep, &createdAtRaw, &updatedAtRaw); err != nil {
			continue
		}
		run.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAtRaw)
		run.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAtRaw)
		out = append(out, run)
	}
	nextCursor := ""
	if len(out) > limit {
		nextCursor = out[limit-1].UpdatedAt.Format(time.RFC3339Nano)
		out = out[:limit]
	}
	return out, nextCursor, nil
}

func (s *Store) LoadRunEventsWindow(ctx context.Context, runID string, afterSeq int64, limit int) ([]StoredRunEvent, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	rows, err := s.db.QueryContext(ctx, `SELECT seq, type, data_json, time FROM run_events WHERE run_id = ? AND seq > ? ORDER BY seq ASC LIMIT ?`, runID, afterSeq, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StoredRunEvent
	for rows.Next() {
		var evt StoredRunEvent
		var timeRaw string
		if err := rows.Scan(&evt.Seq, &evt.Type, &evt.Data, &timeRaw); err != nil {
			continue
		}
		evt.Time, _ = time.Parse(time.RFC3339Nano, timeRaw)
		out = append(out, evt)
	}
	return out, nil
}

func (s *Store) MarkRunStatus(ctx context.Context, runID string, status string, errText string) error {
	if s == nil || s.db == nil {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `UPDATE runs SET status = ?, error = ?, updated_at = ? WHERE id = ?`, status, errText, time.Now().UTC().Format(time.RFC3339Nano), runID)
	if err != nil {
		return fmt.Errorf("update run status: %w", err)
	}
	return nil
}

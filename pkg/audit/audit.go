package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

const (
	EventToolExec    = "tool_exec"
	EventToolApprove = "tool_approve"
	EventToolDeny    = "tool_deny"
	EventMemorySet   = "memory_set"
	EventMemoryDel   = "memory_del"
	EventCredSet     = "credential_set"
	EventCredDel     = "credential_del"
	EventConfigChg   = "config_change"
	EventSessionNew  = "session_new"
	EventSkillLoad   = "skill_load"
)

type Entry struct {
	ID        string
	Timestamp time.Time
	EventType string
	SessionID string
	AgentID   string
	Actor     string
	Detail    string
}

type Logger struct {
	db *sql.DB
}

func New(db *sql.DB) (*Logger, error) {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS audit_log (
		id         TEXT PRIMARY KEY,
		timestamp  TIMESTAMP NOT NULL,
		event_type TEXT NOT NULL,
		session_id TEXT NOT NULL DEFAULT '',
		agent_id   TEXT NOT NULL DEFAULT '',
		actor      TEXT NOT NULL DEFAULT '',
		detail     TEXT NOT NULL DEFAULT ''
	)`)
	if err != nil {
		return nil, fmt.Errorf("audit: creating table: %w", err)
	}

	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_audit_timestamp ON audit_log(timestamp)`)
	if err != nil {
		return nil, fmt.Errorf("audit: creating index: %w", err)
	}

	return &Logger{db: db}, nil
}

func (l *Logger) Log(ctx context.Context, eventType, sessionID, agentID, actor string, detail any) error {
	var detailStr string
	switch v := detail.(type) {
	case string:
		detailStr = v
	default:
		b, err := json.Marshal(v)
		if err != nil {
			detailStr = fmt.Sprintf("%v", v)
		} else {
			detailStr = string(b)
		}
	}

	_, err := l.db.ExecContext(ctx,
		`INSERT INTO audit_log (id, timestamp, event_type, session_id, agent_id, actor, detail)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		uuid.NewString(), time.Now().UTC(), eventType, sessionID, agentID, actor, detailStr,
	)
	return err
}

func (l *Logger) Query(ctx context.Context, f Filter) ([]Entry, error) {
	query := `SELECT id, timestamp, event_type, session_id, agent_id, actor, detail FROM audit_log WHERE 1=1`
	var args []any

	if f.EventType != "" {
		query += ` AND event_type = ?`
		args = append(args, f.EventType)
	}
	if f.SessionID != "" {
		query += ` AND session_id = ?`
		args = append(args, f.SessionID)
	}
	if f.AgentID != "" {
		query += ` AND agent_id = ?`
		args = append(args, f.AgentID)
	}
	if !f.Since.IsZero() {
		query += ` AND timestamp >= ?`
		args = append(args, f.Since)
	}
	if !f.Until.IsZero() {
		query += ` AND timestamp <= ?`
		args = append(args, f.Until)
	}

	query += ` ORDER BY timestamp DESC`

	if f.Limit > 0 {
		query += ` LIMIT ?`
		args = append(args, f.Limit)
	}

	rows, err := l.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []Entry
	for rows.Next() {
		var e Entry
		if err := rows.Scan(&e.ID, &e.Timestamp, &e.EventType, &e.SessionID, &e.AgentID, &e.Actor, &e.Detail); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

type Filter struct {
	EventType string
	SessionID string
	AgentID   string
	Since     time.Time
	Until     time.Time
	Limit     int
}

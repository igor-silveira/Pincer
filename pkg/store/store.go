package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

func New(dsn string) (*Store, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enabling WAL mode: %w", err)
	}

	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enabling foreign keys: %w", err)
	}

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("running migrations: %w", err)
	}

	return s, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) migrate() error {
	for _, stmt := range migrations {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("migration failed: %w\nSQL: %s", err, stmt)
		}
	}
	return nil
}

var migrations = []string{
	`CREATE TABLE IF NOT EXISTS sessions (
	    id         TEXT PRIMARY KEY,
	    agent_id   TEXT NOT NULL,
	    channel    TEXT NOT NULL,
	    peer_id    TEXT NOT NULL,
	    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`,
	`CREATE TABLE IF NOT EXISTS messages (
	    id           TEXT PRIMARY KEY,
	    session_id   TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
	    role         TEXT NOT NULL,
	    content_type TEXT NOT NULL DEFAULT 'text',
	    content      TEXT NOT NULL,
	    token_count  INTEGER NOT NULL DEFAULT 0,
	    created_at   TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`,
	`CREATE INDEX IF NOT EXISTS idx_messages_session ON messages(session_id, created_at)`,
	`CREATE TABLE IF NOT EXISTS memory (
	    id         TEXT PRIMARY KEY,
	    agent_id   TEXT NOT NULL,
	    key        TEXT NOT NULL,
	    value      TEXT NOT NULL,
	    hash       TEXT NOT NULL,
	    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	    UNIQUE(agent_id, key)
	)`,
	`CREATE TABLE IF NOT EXISTS credentials (
	    id              TEXT PRIMARY KEY,
	    name            TEXT NOT NULL UNIQUE,
	    encrypted_value BLOB NOT NULL,
	    created_at      TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`,
}

const (
	ContentTypeText        = "text"
	ContentTypeToolCalls   = "tool_calls"
	ContentTypeToolResults = "tool_results"
)

type Session struct {
	ID        string
	AgentID   string
	Channel   string
	PeerID    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type Message struct {
	ID          string
	SessionID   string
	Role        string
	ContentType string
	Content     string
	TokenCount  int
	CreatedAt   time.Time
}

func (s *Store) CreateSession(ctx context.Context, sess *Session) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (id, agent_id, channel, peer_id, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		sess.ID, sess.AgentID, sess.Channel, sess.PeerID, sess.CreatedAt, sess.UpdatedAt,
	)
	return err
}

func (s *Store) GetSession(ctx context.Context, id string) (*Session, error) {
	sess := &Session{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, agent_id, channel, peer_id, created_at, updated_at FROM sessions WHERE id = ?`, id,
	).Scan(&sess.ID, &sess.AgentID, &sess.Channel, &sess.PeerID, &sess.CreatedAt, &sess.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return sess, nil
}

func (s *Store) FindSession(ctx context.Context, agentID, channel, peerID string) (*Session, error) {
	sess := &Session{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, agent_id, channel, peer_id, created_at, updated_at
		 FROM sessions WHERE agent_id = ? AND channel = ? AND peer_id = ?
		 ORDER BY updated_at DESC LIMIT 1`,
		agentID, channel, peerID,
	).Scan(&sess.ID, &sess.AgentID, &sess.Channel, &sess.PeerID, &sess.CreatedAt, &sess.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return sess, nil
}

func (s *Store) TouchSession(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE sessions SET updated_at = ? WHERE id = ?`, time.Now().UTC(), id,
	)
	return err
}

func (s *Store) AppendMessage(ctx context.Context, msg *Message) error {
	contentType := msg.ContentType
	if contentType == "" {
		contentType = ContentTypeText
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO messages (id, session_id, role, content_type, content, token_count, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		msg.ID, msg.SessionID, msg.Role, contentType, msg.Content, msg.TokenCount, msg.CreatedAt,
	)
	return err
}

func (s *Store) RecentMessages(ctx context.Context, sessionID string, limit int) ([]Message, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, session_id, role, content_type, content, token_count, created_at
		 FROM messages WHERE session_id = ?
		 ORDER BY created_at DESC LIMIT ?`,
		sessionID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.SessionID, &m.Role, &m.ContentType, &m.Content, &m.TokenCount, &m.CreatedAt); err != nil {
			return nil, err
		}
		msgs = append(msgs, m)
	}

	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}

	return msgs, rows.Err()
}

func (s *Store) MessageCount(ctx context.Context, sessionID string) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM messages WHERE session_id = ?`, sessionID,
	).Scan(&count)
	return count, err
}

func (s *Store) SessionTokenUsage(ctx context.Context, sessionID string) (int, error) {
	var total int
	err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(token_count), 0) FROM messages WHERE session_id = ?`, sessionID,
	).Scan(&total)
	return total, err
}

func (s *Store) DeleteMessages(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `DELETE FROM messages WHERE id = ?`)
	if err != nil {
		return fmt.Errorf("preparing statement: %w", err)
	}
	defer stmt.Close()

	for _, id := range ids {
		if _, err := stmt.ExecContext(ctx, id); err != nil {
			return fmt.Errorf("deleting message %s: %w", id, err)
		}
	}

	return tx.Commit()
}

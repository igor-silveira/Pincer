package memory

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Entry struct {
	ID        string
	AgentID   string
	Key       string
	Value     string
	Hash      string
	UpdatedAt time.Time
}

type Store struct {
	db            *sql.DB
	immutableKeys map[string]bool
}

func New(db *sql.DB, immutableKeys []string) *Store {
	ik := make(map[string]bool, len(immutableKeys))
	for _, k := range immutableKeys {
		ik[k] = true
	}
	return &Store{db: db, immutableKeys: ik}
}

func (s *Store) Get(ctx context.Context, agentID, key string) (*Entry, error) {
	e := &Entry{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, agent_id, key, value, hash, updated_at
		 FROM memory WHERE agent_id = ? AND key = ?`,
		agentID, key,
	).Scan(&e.ID, &e.AgentID, &e.Key, &e.Value, &e.Hash, &e.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("memory: %q not found for agent %q", key, agentID)
		}
		return nil, err
	}
	return e, nil
}

func (s *Store) Set(ctx context.Context, agentID, key, value string) error {
	hash := contentHash(value)

	if s.immutableKeys[key] {
		existing, err := s.Get(ctx, agentID, key)
		if err == nil && existing != nil {
			return fmt.Errorf("memory: key %q is immutable and already set", key)
		}
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO memory (id, agent_id, key, value, hash, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(agent_id, key) DO UPDATE SET value = excluded.value, hash = excluded.hash, updated_at = excluded.updated_at`,
		uuid.NewString(), agentID, key, value, hash, time.Now().UTC(),
	)
	return err
}

func (s *Store) Delete(ctx context.Context, agentID, key string) error {
	if s.immutableKeys[key] {
		return fmt.Errorf("memory: key %q is immutable and cannot be deleted", key)
	}

	res, err := s.db.ExecContext(ctx,
		`DELETE FROM memory WHERE agent_id = ? AND key = ?`, agentID, key,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("memory: %q not found for agent %q", key, agentID)
	}
	return nil
}

func (s *Store) List(ctx context.Context, agentID string) ([]Entry, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, agent_id, key, value, hash, updated_at
		 FROM memory WHERE agent_id = ? ORDER BY key`, agentID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []Entry
	for rows.Next() {
		var e Entry
		if err := rows.Scan(&e.ID, &e.AgentID, &e.Key, &e.Value, &e.Hash, &e.UpdatedAt); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

func (s *Store) Search(ctx context.Context, agentID, query string) ([]Entry, error) {
	pattern := "%" + query + "%"
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, agent_id, key, value, hash, updated_at
		 FROM memory WHERE agent_id = ? AND (key LIKE ? OR value LIKE ?)
		 ORDER BY updated_at DESC`, agentID, pattern, pattern,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []Entry
	for rows.Next() {
		var e Entry
		if err := rows.Scan(&e.ID, &e.AgentID, &e.Key, &e.Value, &e.Hash, &e.UpdatedAt); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

func (s *Store) Diff(ctx context.Context, agentID string, since time.Time) ([]Entry, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, agent_id, key, value, hash, updated_at
		 FROM memory WHERE agent_id = ? AND updated_at > ?
		 ORDER BY updated_at ASC`, agentID, since,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []Entry
	for rows.Next() {
		var e Entry
		if err := rows.Scan(&e.ID, &e.AgentID, &e.Key, &e.Value, &e.Hash, &e.UpdatedAt); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

func (s *Store) IsImmutable(key string) bool {
	return s.immutableKeys[key]
}

func (s *Store) BuildContext(ctx context.Context, agentID string, lastHashes map[string]string) (string, map[string]string, error) {
	entries, err := s.List(ctx, agentID)
	if err != nil {
		return "", nil, err
	}

	currentHashes := make(map[string]string, len(entries))
	var parts []string
	for _, e := range entries {
		currentHashes[e.Key] = e.Hash
		if lastHashes[e.Key] == e.Hash {
			continue
		}
		parts = append(parts, fmt.Sprintf("[%s]: %s", e.Key, e.Value))
	}

	if len(parts) == 0 {
		return "", currentHashes, nil
	}

	return strings.Join(parts, "\n"), currentHashes, nil
}

func contentHash(value string) string {
	h := sha256.Sum256([]byte(value))
	return fmt.Sprintf("%x", h[:16])
}

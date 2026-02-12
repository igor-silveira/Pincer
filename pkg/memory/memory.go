package memory

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Entry struct {
	ID        string    `gorm:"primaryKey;column:id"`
	AgentID   string    `gorm:"column:agent_id;not null;uniqueIndex:idx_memory_agent_key"`
	Key       string    `gorm:"column:key;not null;uniqueIndex:idx_memory_agent_key"`
	Value     string    `gorm:"column:value;not null"`
	Hash      string    `gorm:"column:hash;not null"`
	UpdatedAt time.Time `gorm:"column:updated_at;not null"`
}

func (Entry) TableName() string {
	return "memory"
}

type Store struct {
	db            *gorm.DB
	immutableKeys map[string]bool
}

func New(db *gorm.DB, immutableKeys []string) *Store {
	ik := make(map[string]bool, len(immutableKeys))
	for _, k := range immutableKeys {
		ik[k] = true
	}
	return &Store{db: db, immutableKeys: ik}
}

func (s *Store) Get(ctx context.Context, agentID, key string) (*Entry, error) {
	e := &Entry{}
	err := s.db.WithContext(ctx).
		Where("agent_id = ? AND key = ?", agentID, key).
		First(e).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
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

	entry := &Entry{
		ID:        uuid.NewString(),
		AgentID:   agentID,
		Key:       key,
		Value:     value,
		Hash:      hash,
		UpdatedAt: time.Now().UTC(),
	}

	return s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "agent_id"}, {Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value", "hash", "updated_at"}),
	}).Create(entry).Error
}

func (s *Store) Delete(ctx context.Context, agentID, key string) error {
	if s.immutableKeys[key] {
		return fmt.Errorf("memory: key %q is immutable and cannot be deleted", key)
	}

	result := s.db.WithContext(ctx).
		Where("agent_id = ? AND key = ?", agentID, key).
		Delete(&Entry{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("memory: %q not found for agent %q", key, agentID)
	}
	return nil
}

func (s *Store) List(ctx context.Context, agentID string) ([]Entry, error) {
	var entries []Entry
	err := s.db.WithContext(ctx).
		Where("agent_id = ?", agentID).
		Order("key").
		Find(&entries).Error
	return entries, err
}

func (s *Store) Search(ctx context.Context, agentID, query string) ([]Entry, error) {
	pattern := "%" + query + "%"
	var entries []Entry
	err := s.db.WithContext(ctx).
		Where("agent_id = ? AND (key LIKE ? OR value LIKE ?)", agentID, pattern, pattern).
		Order("updated_at DESC").
		Find(&entries).Error
	return entries, err
}

func (s *Store) Diff(ctx context.Context, agentID string, since time.Time) ([]Entry, error) {
	var entries []Entry
	err := s.db.WithContext(ctx).
		Where("agent_id = ? AND updated_at > ?", agentID, since).
		Order("updated_at ASC").
		Find(&entries).Error
	return entries, err
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

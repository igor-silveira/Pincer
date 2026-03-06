package store

import (
	"context"
	"fmt"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type Store struct {
	db *gorm.DB
}

func New(dsn string) (*Store, error) {
	db, err := gorm.Open(sqlite.Open(dsn+"?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"), &gorm.Config{
		Logger: logger.Discard,
	})
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	if err := db.AutoMigrate(&Session{}, &Message{}, &Memory{}, &Credential{}, &Checkpoint{}); err != nil {
		return nil, fmt.Errorf("running migrations: %w", err)
	}

	return &Store{db: db}, nil
}

func (s *Store) DB() *gorm.DB {
	return s.db
}

func (s *Store) Close() error {
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

const (
	ContentTypeText        = "text"
	ContentTypeToolCalls   = "tool_calls"
	ContentTypeToolResults = "tool_results"
)

type Session struct {
	ID        string    `gorm:"primaryKey;column:id"`
	AgentID   string    `gorm:"column:agent_id;not null"`
	Channel   string    `gorm:"column:channel;not null"`
	PeerID    string    `gorm:"column:peer_id;not null"`
	CreatedAt time.Time `gorm:"column:created_at;not null"`
	UpdatedAt time.Time `gorm:"column:updated_at;not null"`
}

type Message struct {
	ID          string    `gorm:"primaryKey;column:id"`
	SessionID   string    `gorm:"column:session_id;not null;index:idx_messages_session"`
	Role        string    `gorm:"column:role;not null"`
	ContentType string    `gorm:"column:content_type;not null;default:text"`
	Content     string    `gorm:"column:content;not null"`
	TokenCount  int       `gorm:"column:token_count;not null;default:0"`
	CreatedAt   time.Time `gorm:"column:created_at;not null;index:idx_messages_session"`
}

type Memory struct {
	ID        string    `gorm:"primaryKey;column:id"`
	AgentID   string    `gorm:"column:agent_id;not null;uniqueIndex:idx_memory_agent_key"`
	Key       string    `gorm:"column:key;not null;uniqueIndex:idx_memory_agent_key"`
	Value     string    `gorm:"column:value;not null"`
	Hash      string    `gorm:"column:hash;not null"`
	UpdatedAt time.Time `gorm:"column:updated_at;not null"`
}

func (Memory) TableName() string {
	return "memory"
}

type Credential struct {
	ID             string    `gorm:"primaryKey;column:id"`
	Name           string    `gorm:"column:name;not null;uniqueIndex"`
	EncryptedValue []byte    `gorm:"column:encrypted_value;not null"`
	CreatedAt      time.Time `gorm:"column:created_at;not null"`
}

func (s *Store) CreateSession(ctx context.Context, sess *Session) error {
	return s.db.WithContext(ctx).Create(sess).Error
}

func (s *Store) GetSession(ctx context.Context, id string) (*Session, error) {
	sess := &Session{}
	err := s.db.WithContext(ctx).First(sess, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return sess, nil
}

func (s *Store) FindSession(ctx context.Context, agentID, channel, peerID string) (*Session, error) {
	sess := &Session{}
	err := s.db.WithContext(ctx).
		Where("agent_id = ? AND channel = ? AND peer_id = ?", agentID, channel, peerID).
		Order("updated_at DESC").
		First(sess).Error
	if err != nil {
		return nil, err
	}
	return sess, nil
}

func (s *Store) GetOrCreateSession(ctx context.Context, id, channel, peerID string) (sess *Session, created bool, err error) {
	sess, err = s.GetSession(ctx, id)
	if err == nil {
		if sess.Channel != channel || sess.PeerID != peerID {
			if err := s.UpdateSessionChannel(ctx, id, channel, peerID); err != nil {
				return sess, false, fmt.Errorf("updating session channel: %w", err)
			}
			sess.Channel = channel
			sess.PeerID = peerID
		}
		return sess, false, nil
	}

	now := time.Now().UTC()
	sess = &Session{
		ID:        id,
		AgentID:   "default",
		Channel:   channel,
		PeerID:    peerID,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.CreateSession(ctx, sess); err != nil {
		return nil, false, fmt.Errorf("creating session: %w", err)
	}
	return sess, true, nil
}

func (s *Store) TouchSession(ctx context.Context, id string) error {
	return s.db.WithContext(ctx).
		Model(&Session{}).
		Where("id = ?", id).
		Update("updated_at", time.Now().UTC()).Error
}

func (s *Store) UpdateSessionChannel(ctx context.Context, id, channel, peerID string) error {
	return s.db.WithContext(ctx).
		Model(&Session{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"channel":    channel,
			"peer_id":    peerID,
			"updated_at": time.Now().UTC(),
		}).Error
}

func (s *Store) AppendMessage(ctx context.Context, msg *Message) error {
	if msg.ContentType == "" {
		msg.ContentType = ContentTypeText
	}
	return s.db.WithContext(ctx).Create(msg).Error
}

func (s *Store) RecentMessages(ctx context.Context, sessionID string, limit int) ([]Message, error) {
	var msgs []Message
	err := s.db.WithContext(ctx).
		Where("session_id = ?", sessionID).
		Order("created_at DESC").
		Limit(limit).
		Find(&msgs).Error
	if err != nil {
		return nil, err
	}

	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}

	return msgs, nil
}

func (s *Store) MessageCount(ctx context.Context, sessionID string) (int64, error) {
	var count int64
	err := s.db.WithContext(ctx).
		Model(&Message{}).
		Where("session_id = ?", sessionID).
		Count(&count).Error
	return count, err
}

func (s *Store) SessionTokenUsage(ctx context.Context, sessionID string) (int, error) {
	var total int
	err := s.db.WithContext(ctx).
		Model(&Message{}).
		Where("session_id = ?", sessionID).
		Select("COALESCE(SUM(token_count), 0)").
		Scan(&total).Error
	return total, err
}

func (s *Store) DeleteMessages(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}

	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return tx.Delete(&Message{}, ids).Error
	})
}

type Checkpoint struct {
	ID             string    `gorm:"primaryKey;column:id"`
	SessionID      string    `gorm:"column:session_id;not null;uniqueIndex:idx_checkpoint_session_step"`
	StepIndex      int       `gorm:"column:step_index;not null;uniqueIndex:idx_checkpoint_session_step"`
	StateSnapshot  string    `gorm:"column:state_snapshot;not null"`
	ToolOutputs    string    `gorm:"column:tool_outputs;not null"`
	ContextSummary string    `gorm:"column:context_summary;not null"`
	CreatedAt      time.Time `gorm:"column:created_at;not null"`
}

func (s *Store) SaveCheckpoint(ctx context.Context, cp *Checkpoint) error {
	if cp.CreatedAt.IsZero() {
		cp.CreatedAt = time.Now().UTC()
	}
	return s.db.WithContext(ctx).Save(cp).Error
}

func (s *Store) LatestCheckpoint(ctx context.Context, sessionID string) (*Checkpoint, error) {
	cp := &Checkpoint{}
	err := s.db.WithContext(ctx).
		Where("session_id = ?", sessionID).
		Order("step_index DESC").
		First(cp).Error
	if err != nil {
		return nil, err
	}
	return cp, nil
}

func (s *Store) CheckpointAtStep(ctx context.Context, sessionID string, step int) (*Checkpoint, error) {
	cp := &Checkpoint{}
	err := s.db.WithContext(ctx).
		Where("session_id = ? AND step_index = ?", sessionID, step).
		First(cp).Error
	if err != nil {
		return nil, err
	}
	return cp, nil
}

func (s *Store) DeleteCheckpointsOlderThan(ctx context.Context, before time.Time) (int64, error) {
	result := s.db.WithContext(ctx).
		Where("created_at < ?", before).
		Delete(&Checkpoint{})
	return result.RowsAffected, result.Error
}

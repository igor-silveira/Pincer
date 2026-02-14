package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
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
	EventSessionNew     = "session_new"
	EventSkillLoad      = "skill_load"
	EventNotifySchedule = "notify_schedule"
	EventNotifyDeliver  = "notify_deliver"
	EventNotifySend     = "notify_send"
	EventMCPConnect     = "mcp_connect"
	EventMCPDisconnect  = "mcp_disconnect"
	EventA2ATaskNew     = "a2a_task_new"
	EventA2ATaskDone    = "a2a_task_done"
	EventA2ATaskFail    = "a2a_task_fail"
	EventA2ATaskCancel  = "a2a_task_cancel"
)

type Entry struct {
	ID        string    `gorm:"primaryKey;column:id"`
	Timestamp time.Time `gorm:"column:timestamp;not null;index:idx_audit_timestamp"`
	EventType string    `gorm:"column:event_type;not null"`
	SessionID string    `gorm:"column:session_id;not null;default:''"`
	AgentID   string    `gorm:"column:agent_id;not null;default:''"`
	Actor     string    `gorm:"column:actor;not null;default:''"`
	Detail    string    `gorm:"column:detail;not null;default:''"`
}

func (Entry) TableName() string {
	return "audit_log"
}

type Logger struct {
	db *gorm.DB
}

func New(db *gorm.DB) (*Logger, error) {
	if err := db.AutoMigrate(&Entry{}); err != nil {
		return nil, fmt.Errorf("audit: running migrations: %w", err)
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

	entry := &Entry{
		ID:        uuid.NewString(),
		Timestamp: time.Now().UTC(),
		EventType: eventType,
		SessionID: sessionID,
		AgentID:   agentID,
		Actor:     actor,
		Detail:    detailStr,
	}

	return l.db.WithContext(ctx).Create(entry).Error
}

func (l *Logger) Query(ctx context.Context, f Filter) ([]Entry, error) {
	q := l.db.WithContext(ctx)

	if f.EventType != "" {
		q = q.Where("event_type = ?", f.EventType)
	}
	if f.SessionID != "" {
		q = q.Where("session_id = ?", f.SessionID)
	}
	if f.AgentID != "" {
		q = q.Where("agent_id = ?", f.AgentID)
	}
	if !f.Since.IsZero() {
		q = q.Where("timestamp >= ?", f.Since)
	}
	if !f.Until.IsZero() {
		q = q.Where("timestamp <= ?", f.Until)
	}

	q = q.Order("timestamp DESC")

	if f.Limit > 0 {
		q = q.Limit(f.Limit)
	}

	var entries []Entry
	err := q.Find(&entries).Error
	return entries, err
}

type Filter struct {
	EventType string
	SessionID string
	AgentID   string
	Since     time.Time
	Until     time.Time
	Limit     int
}

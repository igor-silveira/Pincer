package pincer

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/igorsilveira/pincer/pkg/audit"
	"github.com/igorsilveira/pincer/pkg/config"
	"github.com/igorsilveira/pincer/pkg/store"
	"github.com/spf13/cobra"
)

var auditCmd = &cobra.Command{
	Use:   "audit",
	Short: "View the audit log",
	RunE:  runAudit,
}

var (
	auditEventType string
	auditSessionID string
	auditLimit     int
	auditSince     string
)

func init() {
	auditCmd.Flags().StringVar(&auditEventType, "type", "", "filter by event type")
	auditCmd.Flags().StringVar(&auditSessionID, "session", "", "filter by session ID")
	auditCmd.Flags().IntVar(&auditLimit, "limit", 50, "maximum number of entries")
	auditCmd.Flags().StringVar(&auditSince, "since", "", "show entries since (e.g. 2024-01-01)")
}

func runAudit(cmd *cobra.Command, args []string) error {
	cfg := config.Current()
	dsn := cfg.Store.DSN
	if dsn == "" {
		dsn = filepath.Join(config.DataDir(), "pincer.db")
	}

	db, err := store.New(dsn)
	if err != nil {
		return fmt.Errorf("opening store: %w", err)
	}
	defer func() { _ = db.Close() }()

	auditLog, err := audit.New(db.DB())
	if err != nil {
		return fmt.Errorf("initializing audit logger: %w", err)
	}

	filter := audit.Filter{
		EventType: auditEventType,
		SessionID: auditSessionID,
		Limit:     auditLimit,
	}

	if auditSince != "" {
		t, err := time.Parse("2006-01-02", auditSince)
		if err != nil {
			return fmt.Errorf("invalid --since format (use YYYY-MM-DD): %w", err)
		}
		filter.Since = t
	}

	entries, err := auditLog.Query(context.Background(), filter)
	if err != nil {
		return fmt.Errorf("querying audit log: %w", err)
	}

	if len(entries) == 0 {
		fmt.Println("No audit entries found.")
		return nil
	}

	for _, e := range entries {
		ts := e.Timestamp.Format("2006-01-02 15:04:05")
		fmt.Printf("[%s] %-15s session=%-10s agent=%-10s actor=%-8s %s\n",
			ts, e.EventType, e.SessionID, e.AgentID, e.Actor, e.Detail,
		)
	}

	fmt.Printf("\n%d entries\n", len(entries))
	return nil
}

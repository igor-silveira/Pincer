package scheduler

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestParseSchedule(t *testing.T) {
	tests := []struct {
		input    string
		expected time.Duration
		wantErr  bool
	}{
		{"@hourly", time.Hour, false},
		{"@daily", 24 * time.Hour, false},
		{"@weekly", 7 * 24 * time.Hour, false},
		{"@every 5m", 5 * time.Minute, false},
		{"@every 1h30m", 90 * time.Minute, false},
		{"30s", 30 * time.Second, false},
		{"invalid", 0, true},
	}

	for _, tt := range tests {
		d, err := parseSchedule(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("parseSchedule(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			continue
		}
		if d != tt.expected {
			t.Errorf("parseSchedule(%q) = %v, want %v", tt.input, d, tt.expected)
		}
	}
}

func TestAddJob(t *testing.T) {
	s := New()
	err := s.Add(Job{
		Name:     "test",
		Schedule: "@every 1s",
		Func:     func(ctx context.Context) error { return nil },
	})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
}

func TestAddInvalidSchedule(t *testing.T) {
	s := New()
	err := s.Add(Job{
		Name:     "bad",
		Schedule: "not-a-schedule",
		Func:     func(ctx context.Context) error { return nil },
	})
	if err == nil {
		t.Fatal("expected error for invalid schedule")
	}
}

func TestSchedulerRuns(t *testing.T) {
	s := New()
	var count atomic.Int32

	if err := s.Add(Job{
		Name:     "counter",
		Schedule: "100ms",
		Func: func(ctx context.Context) error {
			count.Add(1)
			return nil
		},
	}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	s.mu.Lock()
	for i := range s.jobs {
		s.jobs[i].next = time.Now().Add(-time.Second)
	}
	s.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	go s.Start(ctx)

	time.Sleep(1500 * time.Millisecond)
	cancel()

	c := count.Load()
	if c < 1 {
		t.Errorf("count = %d, expected at least 1", c)
	}
}

func TestWebhookSignature(t *testing.T) {
	wh := NewWebhookHandler("my-secret")

	body := []byte(`{"event":"test"}`)

	if wh.verifySignature(body, "invalid") {
		t.Fatal("expected false for invalid signature")
	}
	if wh.verifySignature(body, "") {
		t.Fatal("expected false for empty signature")
	}
}

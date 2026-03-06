package executor

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunBatch_ResultOrder(t *testing.T) {
	exec := New(4)
	tasks := make([]Task, 5)
	for i := range tasks {
		id := fmt.Sprintf("task-%d", i)
		tasks[i] = Task{
			ID: id,
			Fn: func(ctx context.Context) (string, error) {
				return id, nil
			},
		}
	}
	br := exec.RunBatch(context.Background(), tasks)
	if len(br.Results) != 5 {
		t.Fatalf("expected 5 results, got %d", len(br.Results))
	}
	for i, r := range br.Results {
		want := fmt.Sprintf("task-%d", i)
		if r.ID != want {
			t.Errorf("result[%d].ID = %q, want %q", i, r.ID, want)
		}
		if r.Err != nil {
			t.Errorf("result[%d].Err = %v, want nil", i, r.Err)
		}
	}
}

func TestRunBatch_ConcurrencyLimit(t *testing.T) {
	exec := New(2)
	var concurrent atomic.Int32
	var maxConcurrent atomic.Int32

	tasks := make([]Task, 6)
	for i := range tasks {
		tasks[i] = Task{
			ID: fmt.Sprintf("t-%d", i),
			Fn: func(ctx context.Context) (string, error) {
				cur := concurrent.Add(1)
				for {
					old := maxConcurrent.Load()
					if cur <= old || maxConcurrent.CompareAndSwap(old, cur) {
						break
					}
				}
				time.Sleep(30 * time.Millisecond)
				concurrent.Add(-1)
				return "ok", nil
			},
		}
	}

	exec.RunBatch(context.Background(), tasks)

	if mc := maxConcurrent.Load(); mc > 2 {
		t.Errorf("max concurrent = %d, want <= 2", mc)
	}
}

func TestRunBatch_PerTaskTimeout(t *testing.T) {
	exec := New(4)
	tasks := []Task{
		{
			ID:      "fast",
			Fn:      func(ctx context.Context) (string, error) { return "ok", nil },
			Timeout: time.Second,
		},
		{
			ID: "slow",
			Fn: func(ctx context.Context) (string, error) {
				select {
				case <-ctx.Done():
					return "", ctx.Err()
				case <-time.After(5 * time.Second):
					return "should not reach", nil
				}
			},
			Timeout: 50 * time.Millisecond,
		},
	}

	br := exec.RunBatch(context.Background(), tasks)

	if br.Results[0].Err != nil {
		t.Errorf("fast task should succeed, got %v", br.Results[0].Err)
	}
	if br.Results[1].Err == nil {
		t.Error("slow task should timeout")
	}
}

func TestRunBatch_Callbacks(t *testing.T) {
	exec := New(4)
	var started, done atomic.Int32

	tasks := []Task{
		{
			ID:      "cb-1",
			Fn:      func(ctx context.Context) (string, error) { return "ok", nil },
			OnStart: func() { started.Add(1) },
			OnDone:  func(r Result) { done.Add(1) },
		},
		{
			ID:      "cb-2",
			Fn:      func(ctx context.Context) (string, error) { return "", fmt.Errorf("fail") },
			OnStart: func() { started.Add(1) },
			OnDone:  func(r Result) { done.Add(1) },
		},
	}

	exec.RunBatch(context.Background(), tasks)

	if started.Load() != 2 {
		t.Errorf("started = %d, want 2", started.Load())
	}
	if done.Load() != 2 {
		t.Errorf("done = %d, want 2", done.Load())
	}
}

func TestRunBatch_ParentCancel(t *testing.T) {
	exec := New(4)
	ctx, cancel := context.WithCancel(context.Background())

	tasks := []Task{
		{
			ID: "blocked",
			Fn: func(ctx context.Context) (string, error) {
				<-ctx.Done()
				return "", ctx.Err()
			},
		},
	}

	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	br := exec.RunBatch(ctx, tasks)
	if br.Results[0].Err == nil {
		t.Error("expected context cancellation error")
	}
}

func TestRunBatch_Empty(t *testing.T) {
	exec := New(4)
	br := exec.RunBatch(context.Background(), nil)
	if len(br.Results) != 0 {
		t.Errorf("expected 0 results for empty batch, got %d", len(br.Results))
	}
}

func TestRunBatch_ElapsedTracking(t *testing.T) {
	exec := New(4)
	tasks := []Task{
		{
			ID: "timed",
			Fn: func(ctx context.Context) (string, error) {
				time.Sleep(20 * time.Millisecond)
				return "ok", nil
			},
		},
	}
	br := exec.RunBatch(context.Background(), tasks)
	if br.Results[0].Elapsed < 15*time.Millisecond {
		t.Errorf("elapsed = %v, expected >= 15ms", br.Results[0].Elapsed)
	}
}

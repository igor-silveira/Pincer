package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/igorsilveira/pincer/pkg/telemetry"
)

type Job struct {
	Name     string
	Schedule string
	Func     func(ctx context.Context) error
}

type Scheduler struct {
	jobs    []entry
	mu      sync.Mutex
	running bool
	stopCh  chan struct{}
}

type entry struct {
	job      Job
	interval time.Duration
	next     time.Time
}

func New() *Scheduler {
	return &Scheduler{
		stopCh: make(chan struct{}),
	}
}

func (s *Scheduler) Add(job Job) error {
	interval, err := parseSchedule(job.Schedule)
	if err != nil {
		return fmt.Errorf("scheduler: invalid schedule %q: %w", job.Schedule, err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.jobs = append(s.jobs, entry{
		job:      job,
		interval: interval,
		next:     time.Now().Add(interval),
	})
	return nil
}

func (s *Scheduler) Start(ctx context.Context) {
	s.mu.Lock()
	s.running = true
	s.mu.Unlock()

	logger := telemetry.FromContext(ctx)
	logger.Info("scheduler started", slog.Int("jobs", len(s.jobs)))

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopCh:
			return
		case now := <-ticker.C:
			s.tick(ctx, now, logger)
		}
	}
}

func (s *Scheduler) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		close(s.stopCh)
		s.running = false
	}
}

func (s *Scheduler) tick(ctx context.Context, now time.Time, logger *slog.Logger) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.jobs {
		e := &s.jobs[i]
		if now.Before(e.next) {
			continue
		}

		e.next = now.Add(e.interval)
		go func(j Job) {
			logger.Info("scheduler: running job", slog.String("job", j.Name))
			if err := j.Func(ctx); err != nil {
				logger.Error("scheduler: job failed",
					slog.String("job", j.Name),
					slog.String("err", err.Error()),
				)
			}
		}(e.job)
	}
}

func parseSchedule(s string) (time.Duration, error) {
	switch s {
	case "@hourly":
		return time.Hour, nil
	case "@daily":
		return 24 * time.Hour, nil
	case "@weekly":
		return 7 * 24 * time.Hour, nil
	}

	if len(s) > 7 && s[:7] == "@every " {
		return time.ParseDuration(s[7:])
	}

	return time.ParseDuration(s)
}

package retry

import (
	"fmt"
	"strings"
	"time"
)

// AttemptRecord captures what happened during a single retry attempt.
type AttemptRecord struct {
	Strategy string
	Attempt  int
	Error    string
	Duration time.Duration
}

func (r AttemptRecord) String() string {
	return fmt.Sprintf("attempt %d (%s): %s [%s]", r.Attempt, r.Strategy, r.Error, r.Duration.Round(time.Millisecond))
}

// AttemptLog collects records across retry attempts for a single iteration.
type AttemptLog struct {
	records []AttemptRecord
}

func (l *AttemptLog) Add(r AttemptRecord) {
	l.records = append(l.records, r)
}

func (l *AttemptLog) Count() int { return len(l.records) }

func (l *AttemptLog) Records() []AttemptRecord {
	out := make([]AttemptRecord, len(l.records))
	copy(out, l.records)
	return out
}

func (l *AttemptLog) Summary() string {
	if len(l.records) == 0 {
		return "no retry attempts"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d retry attempt(s):\n", len(l.records))
	for _, r := range l.records {
		fmt.Fprintf(&b, "  - %s\n", r.String())
	}
	return b.String()
}

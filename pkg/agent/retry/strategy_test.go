package retry

import (
	"testing"
)

func TestRotator_NextReturnsStrategiesByPriority(t *testing.T) {
	low := &stubStrategy{name: "low", prio: 1, canApply: true}
	high := &stubStrategy{name: "high", prio: 10, canApply: true}

	rot := NewRotator([]Strategy{low, high}, 3)

	s, rt := rot.Next(TaskContext{}, nil)
	if s == nil {
		t.Fatal("expected a strategy, got nil")
	}
	if s.Name() != "high" {
		t.Errorf("first strategy = %q, want %q", s.Name(), "high")
	}
	if rt == nil || rt.Prompt != "reframed by high" {
		t.Errorf("first reframed task = %v, want prompt %q", rt, "reframed by high")
	}

	s, rt = rot.Next(TaskContext{}, nil)
	if s == nil {
		t.Fatal("expected a strategy, got nil")
	}
	if s.Name() != "low" {
		t.Errorf("second strategy = %q, want %q", s.Name(), "low")
	}
	if rt == nil || rt.Prompt != "reframed by low" {
		t.Errorf("second reframed task = %v, want prompt %q", rt, "reframed by low")
	}

	s, rt = rot.Next(TaskContext{}, nil)
	if s != nil {
		t.Errorf("third call should return nil (exhausted), got %q", s.Name())
	}
	if rt != nil {
		t.Errorf("third call reframed task should be nil, got %v", rt)
	}
}

func TestRotator_SkipsInapplicable(t *testing.T) {
	skip := &stubStrategy{name: "skip", prio: 10, canApply: false}
	use := &stubStrategy{name: "use", prio: 1, canApply: true}

	rot := NewRotator([]Strategy{skip, use}, 3)

	s, rt := rot.Next(TaskContext{}, nil)
	if s == nil || s.Name() != "use" {
		t.Errorf("expected %q, got %v", "use", s)
	}
	if rt == nil || rt.Prompt != "reframed by use" {
		t.Errorf("reframed task = %v, want prompt %q", rt, "reframed by use")
	}
}

func TestRotator_RespectsMaxAttempts(t *testing.T) {
	a := &stubStrategy{name: "a", prio: 10, canApply: true}
	b := &stubStrategy{name: "b", prio: 5, canApply: true}
	c := &stubStrategy{name: "c", prio: 1, canApply: true}

	rot := NewRotator([]Strategy{a, b, c}, 2)

	_, _ = rot.Next(TaskContext{}, nil) // attempt 1
	_, _ = rot.Next(TaskContext{}, nil) // attempt 2

	s, _ := rot.Next(TaskContext{}, nil) // attempt 3 - exceeds max
	if s != nil {
		t.Errorf("expected nil after max attempts, got %q", s.Name())
	}
}

func TestRotator_Reset(t *testing.T) {
	a := &stubStrategy{name: "a", prio: 10, canApply: true}
	rot := NewRotator([]Strategy{a}, 3)

	_, _ = rot.Next(TaskContext{}, nil)
	rot.Reset()

	s, _ := rot.Next(TaskContext{}, nil)
	if s == nil || s.Name() != "a" {
		t.Errorf("after reset expected %q, got %v", "a", s)
	}
}

type stubStrategy struct {
	name     string
	prio     int
	canApply bool
}

func (s *stubStrategy) Name() string  { return s.name }
func (s *stubStrategy) Priority() int { return s.prio }
func (s *stubStrategy) Reframe(tc TaskContext, errs []error) (*ReframedTask, bool) {
	if !s.canApply {
		return nil, false
	}
	return &ReframedTask{Prompt: "reframed by " + s.name}, true
}

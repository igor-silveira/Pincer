package a2a

import (
	"fmt"
	"sync"
)

type TaskStore struct {
	mu    sync.RWMutex
	tasks map[string]*Task
}

func NewTaskStore() *TaskStore {
	return &TaskStore{tasks: make(map[string]*Task)}
}

func (s *TaskStore) Create(task *Task) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tasks[task.ID] = task
}

func (s *TaskStore) Get(id string) (*Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.tasks[id]
	if !ok {
		return nil, fmt.Errorf("task %q not found", id)
	}
	return t, nil
}

func (s *TaskStore) Update(id string, state TaskState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok {
		return fmt.Errorf("task %q not found", id)
	}
	t.State = state
	return nil
}

func (s *TaskStore) AppendMessage(id string, msg Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok {
		return fmt.Errorf("task %q not found", id)
	}
	t.Messages = append(t.Messages, msg)
	return nil
}

func (s *TaskStore) List() []*Task {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]*Task, 0, len(s.tasks))
	for _, t := range s.tasks {
		result = append(result, t)
	}
	return result
}

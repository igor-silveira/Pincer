package a2a

import (
	"fmt"
	"sync"
	"testing"
)

func TestTaskStore_CreateAndGet(t *testing.T) {
	s := NewTaskStore()
	task := &Task{ID: "t1", State: TaskStateSubmitted}
	s.Create(task)

	got, err := s.Get("t1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != "t1" {
		t.Errorf("ID = %q, want %q", got.ID, "t1")
	}
	if got.State != TaskStateSubmitted {
		t.Errorf("State = %q, want %q", got.State, TaskStateSubmitted)
	}
}

func TestTaskStore_GetNotFound(t *testing.T) {
	s := NewTaskStore()
	_, err := s.Get("nonexistent")
	if err == nil {
		t.Error("expected error for missing task")
	}
}

func TestTaskStore_Update(t *testing.T) {
	s := NewTaskStore()
	s.Create(&Task{ID: "t1", State: TaskStateSubmitted})

	if err := s.Update("t1", TaskStateWorking); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, _ := s.Get("t1")
	if got.State != TaskStateWorking {
		t.Errorf("State = %q, want %q", got.State, TaskStateWorking)
	}
}

func TestTaskStore_UpdateNotFound(t *testing.T) {
	s := NewTaskStore()
	if err := s.Update("nonexistent", TaskStateWorking); err == nil {
		t.Error("expected error for missing task")
	}
}

func TestTaskStore_AppendMessage(t *testing.T) {
	s := NewTaskStore()
	s.Create(&Task{ID: "t1", State: TaskStateSubmitted})

	msg := Message{Role: "user", Parts: []Part{{Type: "text", Text: "hello"}}}
	if err := s.AppendMessage("t1", msg); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}

	got, _ := s.Get("t1")
	if len(got.Messages) != 1 {
		t.Fatalf("Messages len = %d, want 1", len(got.Messages))
	}
	if got.Messages[0].Parts[0].Text != "hello" {
		t.Errorf("Text = %q, want %q", got.Messages[0].Parts[0].Text, "hello")
	}
}

func TestTaskStore_AppendMessageNotFound(t *testing.T) {
	s := NewTaskStore()
	msg := Message{Role: "user", Parts: []Part{{Type: "text", Text: "hello"}}}
	if err := s.AppendMessage("nonexistent", msg); err == nil {
		t.Error("expected error for missing task")
	}
}

func TestTaskStore_List(t *testing.T) {
	s := NewTaskStore()
	s.Create(&Task{ID: "t1", State: TaskStateSubmitted})
	s.Create(&Task{ID: "t2", State: TaskStateWorking})
	s.Create(&Task{ID: "t3", State: TaskStateCompleted})

	tasks := s.List()
	if len(tasks) != 3 {
		t.Errorf("List len = %d, want 3", len(tasks))
	}
}

func TestTaskStore_ListEmpty(t *testing.T) {
	s := NewTaskStore()
	tasks := s.List()
	if len(tasks) != 0 {
		t.Errorf("List len = %d, want 0", len(tasks))
	}
}

func TestTaskStore_ConcurrentAccess(t *testing.T) {
	s := NewTaskStore()
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			s.Create(&Task{ID: id, State: TaskStateSubmitted})
		}(fmt.Sprintf("t%d", i))
	}
	wg.Wait()

	tasks := s.List()
	if len(tasks) != 100 {
		t.Errorf("List len = %d, want 100", len(tasks))
	}

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			_, _ = s.Get(id)
			_ = s.Update(id, TaskStateWorking)
		}(fmt.Sprintf("t%d", i))
	}
	wg.Wait()
}

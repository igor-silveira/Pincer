package soul

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/igorsilveira/pincer/pkg/memory"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestDefault_Identity(t *testing.T) {
	s := Default()
	if s.Identity.Name != "Pincer" {
		t.Errorf("Name = %q, want %q", s.Identity.Name, "Pincer")
	}
	if s.Identity.Role != "AI assistant" {
		t.Errorf("Role = %q, want %q", s.Identity.Role, "AI assistant")
	}
}

func TestDefault_Values(t *testing.T) {
	s := Default()
	found := false
	for _, v := range s.Values.Core {
		if v == "honesty" {
			found = true
		}
	}
	if !found {
		t.Errorf("Core values %v should contain 'honesty'", s.Values.Core)
	}
}

func TestRender_ContainsName(t *testing.T) {
	s := Default()
	output := s.Render()
	if !strings.Contains(output, "Pincer") {
		t.Errorf("Render() should contain 'Pincer', got %q", output)
	}
}

func TestRender_ContainsAllSections(t *testing.T) {
	s := Default()
	output := s.Render()
	checks := []string{"Personality:", "Core values:", "Communication style:", "Areas of expertise:", "Always refuse to:"}
	for _, check := range checks {
		if !strings.Contains(output, check) {
			t.Errorf("Render() should contain %q", check)
		}
	}
}

func TestSection_Identity(t *testing.T) {
	s := Default()
	output := s.Section("identity")
	if !strings.Contains(output, "Name: Pincer") {
		t.Errorf("identity section should contain 'Name: Pincer', got %q", output)
	}
	if !strings.Contains(output, "Role: AI assistant") {
		t.Errorf("identity section should contain 'Role: AI assistant', got %q", output)
	}
}

func TestSection_Values(t *testing.T) {
	s := Default()
	output := s.Section("values")
	if !strings.Contains(output, "Core:") {
		t.Errorf("values section should contain 'Core:', got %q", output)
	}
}

func TestSection_Unknown(t *testing.T) {
	s := Default()
	output := s.Section("nonexistent")
	expected := s.Render()
	if output != expected {
		t.Error("unknown section should fall back to Render()")
	}
}

func TestLoad_NonExistent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nonexistent.toml")
	s, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if s.Identity.Name != "Pincer" {
		t.Errorf("non-existent file should return Default(), got Name=%q", s.Identity.Name)
	}
}

func TestSeedMemory(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatalf("opening test db: %v", err)
	}
	if err := db.AutoMigrate(&memory.Entry{}); err != nil {
		t.Fatalf("migrating: %v", err)
	}
	mem := memory.New(db, nil)

	s := Default()
	s.MemorySeeds = []MemorySeed{
		{Key: "greeting", Value: "Hello!"},
		{Key: "farewell", Value: "Goodbye!"},
	}

	ctx := context.Background()
	if err := s.SeedMemory(ctx, mem, "test-agent"); err != nil {
		t.Fatalf("SeedMemory: %v", err)
	}

	entry, err := mem.Get(ctx, "test-agent", "greeting")
	if err != nil {
		t.Fatalf("Get greeting: %v", err)
	}
	if entry.Value != "Hello!" {
		t.Errorf("greeting = %q, want %q", entry.Value, "Hello!")
	}
}

package llm

import (
	"os"
	"testing"
)

func TestNewOllamaProvider_DefaultURL(t *testing.T) {
	orig := os.Getenv("OLLAMA_BASE_URL")
	_ = os.Unsetenv("OLLAMA_BASE_URL")
	defer func() { _ = os.Setenv("OLLAMA_BASE_URL", orig) }()

	p, err := NewOllamaProvider("", "")
	if err != nil {
		t.Fatalf("NewOllamaProvider: %v", err)
	}
	if p.inner.baseURL != ollamaDefaultURL {
		t.Errorf("baseURL = %q, want %q", p.inner.baseURL, ollamaDefaultURL)
	}
}

func TestNewOllamaProvider_DefaultModel(t *testing.T) {
	p, err := NewOllamaProvider("", "")
	if err != nil {
		t.Fatalf("NewOllamaProvider: %v", err)
	}
	if p.model != "llama3" {
		t.Errorf("model = %q, want %q", p.model, "llama3")
	}
}

func TestOllamaProvider_NameAndCapabilities(t *testing.T) {
	p, err := NewOllamaProvider("", "")
	if err != nil {
		t.Fatalf("NewOllamaProvider: %v", err)
	}
	if p.Name() != "ollama" {
		t.Errorf("Name() = %q, want %q", p.Name(), "ollama")
	}
	if !p.SupportsStreaming() {
		t.Error("SupportsStreaming() should be true")
	}
	if !p.SupportsToolUse() {
		t.Error("SupportsToolUse() should be true")
	}
}

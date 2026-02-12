package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefault(t *testing.T) {
	cfg := Default()
	if cfg.Gateway.Port != 18789 {
		t.Errorf("Port = %d, want 18789", cfg.Gateway.Port)
	}
	if cfg.Sandbox.Mode != "process" {
		t.Errorf("Sandbox.Mode = %q, want %q", cfg.Sandbox.Mode, "process")
	}
	if cfg.Agent.Model != "claude-sonnet-4-20250514" {
		t.Errorf("Agent.Model = %q, want default model", cfg.Agent.Model)
	}
}

func TestLoadNonExistent(t *testing.T) {
	cfg, err := Load("/tmp/nonexistent-pincer-config.toml")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Gateway.Port != 18789 {
		t.Errorf("Port = %d, want 18789", cfg.Gateway.Port)
	}
}

func TestLoadValid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pincer.toml")

	content := `
[gateway]
port = 9999
bind = "lan"

[agent]
model = "gpt-4o"
tool_approval = "auto"

[sandbox]
mode = "container"

[memory]
immutable_keys = ["identity"]
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Gateway.Port != 9999 {
		t.Errorf("Port = %d, want 9999", cfg.Gateway.Port)
	}
	if cfg.Gateway.Bind != "lan" {
		t.Errorf("Bind = %q, want %q", cfg.Gateway.Bind, "lan")
	}
	if cfg.Agent.Model != "gpt-4o" {
		t.Errorf("Model = %q, want %q", cfg.Agent.Model, "gpt-4o")
	}
	if cfg.Sandbox.Mode != "container" {
		t.Errorf("Sandbox.Mode = %q, want %q", cfg.Sandbox.Mode, "container")
	}
	if len(cfg.Memory.ImmutableKeys) != 1 || cfg.Memory.ImmutableKeys[0] != "identity" {
		t.Errorf("ImmutableKeys = %v, want [identity]", cfg.Memory.ImmutableKeys)
	}
}

func TestLoadInvalid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.toml")
	os.WriteFile(path, []byte("not [valid toml"), 0644)

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for invalid TOML")
	}
}

func TestCurrent(t *testing.T) {
	cfg := Current()
	if cfg == nil {
		t.Fatal("Current returned nil")
	}
}

func TestDataDir(t *testing.T) {
	dir := DataDir()
	if dir == "" {
		t.Fatal("DataDir returned empty")
	}
}

func TestDataDirEnv(t *testing.T) {
	t.Setenv("PINCER_DATA_DIR", "/tmp/custom-pincer")
	dir := DataDir()
	if dir != "/tmp/custom-pincer" {
		t.Errorf("DataDir = %q, want /tmp/custom-pincer", dir)
	}
}

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
	path := filepath.Join(dir, "pincer.toml.example")

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
	if err := os.WriteFile(path, []byte("not [valid toml"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

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

func TestLoadRetryConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "retry.toml")

	content := `
[agent.retry]
max_attempts = 5
strategies = ["tool_swap", "decompose", "rephrase"]
cooldown_ms = 1000
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Agent.Retry.MaxAttempts != 5 {
		t.Errorf("MaxAttempts = %d, want 5", cfg.Agent.Retry.MaxAttempts)
	}
	if cfg.Agent.Retry.CooldownMS != 1000 {
		t.Errorf("CooldownMS = %d, want 1000", cfg.Agent.Retry.CooldownMS)
	}
	if len(cfg.Agent.Retry.Strategies) != 3 {
		t.Errorf("Strategies len = %d, want 3", len(cfg.Agent.Retry.Strategies))
	}
}

func TestDefaultRetryConfig(t *testing.T) {
	cfg := Default()
	if cfg.Agent.Retry.MaxAttempts != 3 {
		t.Errorf("default MaxAttempts = %d, want 3", cfg.Agent.Retry.MaxAttempts)
	}
	if len(cfg.Agent.Retry.Strategies) != 3 {
		t.Errorf("default Strategies len = %d, want 3", len(cfg.Agent.Retry.Strategies))
	}
}

func TestLoadCheckpointConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "checkpoint.toml")
	content := `
[agent.checkpoint]
enabled = true
token_threshold = 20000
retention_hours = 48
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.Agent.Checkpoint.Enabled {
		t.Error("Checkpoint.Enabled should be true")
	}
	if cfg.Agent.Checkpoint.TokenThreshold != 20000 {
		t.Errorf("TokenThreshold = %d, want 20000", cfg.Agent.Checkpoint.TokenThreshold)
	}
	if cfg.Agent.Checkpoint.RetentionHours != 48 {
		t.Errorf("RetentionHours = %d, want 48", cfg.Agent.Checkpoint.RetentionHours)
	}
}

func TestLoadVerificationConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "verify.toml")
	content := `
[agent.verification]
enabled = true
confidence_threshold = 0.9
max_attempts = 3
gates = ["llm_self_check", "command_output"]
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.Agent.Verification.Enabled {
		t.Error("Verification.Enabled should be true")
	}
	if cfg.Agent.Verification.ConfidenceThreshold != 0.9 {
		t.Errorf("ConfidenceThreshold = %f, want 0.9", cfg.Agent.Verification.ConfidenceThreshold)
	}
	if cfg.Agent.Verification.MaxAttempts != 3 {
		t.Errorf("MaxAttempts = %d, want 3", cfg.Agent.Verification.MaxAttempts)
	}
}

func TestDataDirEnv(t *testing.T) {
	t.Setenv("PINCER_DATA_DIR", "/tmp/custom-pincer")
	dir := DataDir()
	if dir != "/tmp/custom-pincer" {
		t.Errorf("DataDir = %q, want /tmp/custom-pincer", dir)
	}
}

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/igorsilveira/pincer/pkg/sandbox"
)

func spawnCtx() context.Context {
	return WithSessionInfo(context.Background(), "sess-spawn", "agent-1")
}

func TestSpawnTool_StartSuccess(t *testing.T) {
	var gotSessionID, gotPrompt string
	var gotTools []string
	tool := &SpawnTool{
		RunSpawn: func(_ context.Context, sessionID, prompt string, allowedTools []string) string {
			gotSessionID = sessionID
			gotPrompt = prompt
			gotTools = allowedTools
			return "spawn-123"
		},
	}

	input, _ := json.Marshal(spawnInput{Action: "start", Task: "background work"})
	output, err := tool.Execute(spawnCtx(), input, nil, sandbox.Policy{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(output, "spawn-123") {
		t.Errorf("output = %q, want spawn ID", output)
	}
	if gotSessionID != "sess-spawn" {
		t.Errorf("sessionID = %q, want %q", gotSessionID, "sess-spawn")
	}
	if gotPrompt != "background work" {
		t.Errorf("prompt = %q, want %q", gotPrompt, "background work")
	}
	if gotTools != nil {
		t.Errorf("tools = %v, want nil", gotTools)
	}
}

func TestSpawnTool_StartDefaultAction(t *testing.T) {
	tool := &SpawnTool{
		RunSpawn: func(_ context.Context, _, _ string, _ []string) string {
			return "spawn-default"
		},
	}

	input, _ := json.Marshal(spawnInput{Task: "work"})
	output, err := tool.Execute(spawnCtx(), input, nil, sandbox.Policy{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(output, "spawn-default") {
		t.Errorf("output = %q, want spawn ID", output)
	}
}

func TestSpawnTool_StartEmptyTask(t *testing.T) {
	tool := &SpawnTool{
		RunSpawn: func(_ context.Context, _, _ string, _ []string) string {
			return "nope"
		},
	}

	input, _ := json.Marshal(spawnInput{Action: "start", Task: ""})
	_, err := tool.Execute(spawnCtx(), input, nil, sandbox.Policy{})
	if err == nil {
		t.Error("expected error for empty task")
	}
}

func TestSpawnTool_StartNoSession(t *testing.T) {
	tool := &SpawnTool{
		RunSpawn: func(_ context.Context, _, _ string, _ []string) string {
			return "nope"
		},
	}

	input, _ := json.Marshal(spawnInput{Action: "start", Task: "work"})
	_, err := tool.Execute(context.Background(), input, nil, sandbox.Policy{})
	if err == nil {
		t.Error("expected error for missing session")
	}
	if !strings.Contains(err.Error(), "no session") {
		t.Errorf("error = %q, want 'no session' message", err.Error())
	}
}

func TestSpawnTool_CheckDone(t *testing.T) {
	tool := &SpawnTool{
		CheckSpawn: func(spawnID string) (string, bool, error) {
			if spawnID == "spawn-done" {
				return "the result", true, nil
			}
			return "", false, fmt.Errorf("unknown")
		},
	}

	input, _ := json.Marshal(spawnInput{Action: "check", SpawnID: "spawn-done"})
	output, err := tool.Execute(spawnCtx(), input, nil, sandbox.Policy{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if output != "the result" {
		t.Errorf("output = %q, want %q", output, "the result")
	}
}

func TestSpawnTool_CheckStillRunning(t *testing.T) {
	tool := &SpawnTool{
		CheckSpawn: func(_ string) (string, bool, error) {
			return "", false, nil
		},
	}

	input, _ := json.Marshal(spawnInput{Action: "check", SpawnID: "spawn-running"})
	output, err := tool.Execute(spawnCtx(), input, nil, sandbox.Policy{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if output != "still running" {
		t.Errorf("output = %q, want %q", output, "still running")
	}
}

func TestSpawnTool_CheckUnknownID(t *testing.T) {
	tool := &SpawnTool{
		CheckSpawn: func(_ string) (string, bool, error) {
			return "", false, fmt.Errorf("unknown spawn ID: bad-id")
		},
	}

	input, _ := json.Marshal(spawnInput{Action: "check", SpawnID: "bad-id"})
	_, err := tool.Execute(spawnCtx(), input, nil, sandbox.Policy{})
	if err == nil {
		t.Error("expected error for unknown spawn ID")
	}
}

func TestSpawnTool_CheckMissingSpawnID(t *testing.T) {
	tool := &SpawnTool{
		CheckSpawn: func(_ string) (string, bool, error) {
			return "", false, nil
		},
	}

	input, _ := json.Marshal(spawnInput{Action: "check", SpawnID: ""})
	_, err := tool.Execute(spawnCtx(), input, nil, sandbox.Policy{})
	if err == nil {
		t.Error("expected error for missing spawn_id")
	}
}

func TestSpawnTool_UnknownAction(t *testing.T) {
	tool := &SpawnTool{}
	input, _ := json.Marshal(spawnInput{Action: "restart"})
	_, err := tool.Execute(spawnCtx(), input, nil, sandbox.Policy{})
	if err == nil {
		t.Error("expected error for unknown action")
	}
}

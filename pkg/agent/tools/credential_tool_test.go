package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/igorsilveira/pincer/pkg/credentials"
	"github.com/igorsilveira/pincer/pkg/sandbox"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newTestCredentialStore(t *testing.T) *credentials.Store {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatalf("opening test db: %v", err)
	}
	if err := db.AutoMigrate(&credentials.Credential{}); err != nil {
		t.Fatalf("migrating: %v", err)
	}
	store, err := credentials.New(db, "test-master-key")
	if err != nil {
		t.Fatalf("creating credential store: %v", err)
	}
	return store
}

func TestCredentialTool_Set(t *testing.T) {
	tool := &CredentialTool{Credentials: newTestCredentialStore(t)}
	input, _ := json.Marshal(credentialInput{Action: "set", Name: "api_key", Value: "secret123"})

	output, err := tool.Execute(context.Background(), input, nil, sandbox.Policy{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(output, "api_key") || !strings.Contains(output, "stored") {
		t.Errorf("output = %q, want confirmation with name", output)
	}
}

func TestCredentialTool_Get(t *testing.T) {
	store := newTestCredentialStore(t)
	ctx := context.Background()
	_ = store.Set(ctx, "token", "mytoken")

	tool := &CredentialTool{Credentials: store}
	input, _ := json.Marshal(credentialInput{Action: "get", Name: "token"})

	output, err := tool.Execute(ctx, input, nil, sandbox.Policy{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if output != "mytoken" {
		t.Errorf("output = %q, want %q", output, "mytoken")
	}
}

func TestCredentialTool_GetNotFound(t *testing.T) {
	tool := &CredentialTool{Credentials: newTestCredentialStore(t)}
	input, _ := json.Marshal(credentialInput{Action: "get", Name: "missing"})

	_, err := tool.Execute(context.Background(), input, nil, sandbox.Policy{})
	if err == nil {
		t.Error("expected error for missing credential")
	}
}

func TestCredentialTool_Delete(t *testing.T) {
	store := newTestCredentialStore(t)
	ctx := context.Background()
	_ = store.Set(ctx, "temp", "val")

	tool := &CredentialTool{Credentials: store}
	input, _ := json.Marshal(credentialInput{Action: "delete", Name: "temp"})

	output, err := tool.Execute(ctx, input, nil, sandbox.Policy{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(output, "temp") || !strings.Contains(output, "deleted") {
		t.Errorf("output = %q, want confirmation", output)
	}
}

func TestCredentialTool_DeleteNotFound(t *testing.T) {
	tool := &CredentialTool{Credentials: newTestCredentialStore(t)}
	input, _ := json.Marshal(credentialInput{Action: "delete", Name: "gone"})

	_, err := tool.Execute(context.Background(), input, nil, sandbox.Policy{})
	if err == nil {
		t.Error("expected error for deleting missing credential")
	}
}

func TestCredentialTool_ListEmpty(t *testing.T) {
	tool := &CredentialTool{Credentials: newTestCredentialStore(t)}
	input, _ := json.Marshal(credentialInput{Action: "list"})

	output, err := tool.Execute(context.Background(), input, nil, sandbox.Policy{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if output != "no credentials stored" {
		t.Errorf("output = %q, want %q", output, "no credentials stored")
	}
}

func TestCredentialTool_ListWithEntries(t *testing.T) {
	store := newTestCredentialStore(t)
	ctx := context.Background()
	_ = store.Set(ctx, "key1", "v1")
	_ = store.Set(ctx, "key2", "v2")

	tool := &CredentialTool{Credentials: store}
	input, _ := json.Marshal(credentialInput{Action: "list"})

	output, err := tool.Execute(ctx, input, nil, sandbox.Policy{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(output, "key1") || !strings.Contains(output, "key2") {
		t.Errorf("output = %q, want both names listed", output)
	}
}

func TestCredentialTool_UnknownAction(t *testing.T) {
	tool := &CredentialTool{Credentials: newTestCredentialStore(t)}
	input, _ := json.Marshal(credentialInput{Action: "rotate"})

	_, err := tool.Execute(context.Background(), input, nil, sandbox.Policy{})
	if err == nil {
		t.Error("expected error for unknown action")
	}
}

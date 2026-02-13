package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/igorsilveira/pincer/pkg/sandbox"
)

func TestHTTPTool_NetworkDenyBlocks(t *testing.T) {
	tool := &HTTPTool{}
	input, _ := json.Marshal(httpInput{URL: "http://example.com"})
	policy := sandbox.Policy{NetworkAccess: sandbox.NetworkDeny}

	_, err := tool.Execute(context.Background(), input, nil, policy)
	if err == nil {
		t.Fatal("expected error when network access is denied")
	}
}

func TestHTTPTool_NetworkAllowPasses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte("ok"))
	}))
	defer srv.Close()

	tool := &HTTPTool{}
	input, _ := json.Marshal(httpInput{URL: srv.URL})
	policy := sandbox.Policy{NetworkAccess: sandbox.NetworkAllow}

	result, err := tool.Execute(context.Background(), input, nil, policy)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty result")
	}
}

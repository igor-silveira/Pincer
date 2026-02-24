package tools

import (
	"context"
	"encoding/json"
	"os/exec"
	"testing"
	"time"

	"github.com/igorsilveira/pincer/pkg/llm"
	"github.com/igorsilveira/pincer/pkg/sandbox"
)

func TestBrowserTool_Definition(t *testing.T) {
	bt := &BrowserTool{}
	def := bt.Definition()
	if def.Name != "browser" {
		t.Errorf("Name = %q, want %q", def.Name, "browser")
	}
	if len(def.InputSchema) == 0 {
		t.Error("InputSchema should not be empty")
	}
}

func TestBrowserTool_ParseAction(t *testing.T) {
	bt := &BrowserTool{DataDir: t.TempDir(), Headless: true}

	ctx := WithSessionInfo(context.Background(), "test-session", "default")

	input, _ := json.Marshal(browserInput{Action: "unknown"})
	_, err := bt.Execute(ctx, input, nil, sandbox.Policy{})
	if err == nil {
		t.Error("expected error for unknown action")
	}
}

func TestBrowserTool_NoSession(t *testing.T) {
	bt := &BrowserTool{DataDir: t.TempDir(), Headless: true}

	input, _ := json.Marshal(browserInput{Action: "screenshot"})
	_, err := bt.Execute(context.Background(), input, nil, sandbox.Policy{})
	if err == nil {
		t.Error("expected error when no session in context")
	}
}

func TestBrowserTool_NavigateRequiresURL(t *testing.T) {
	bt := &BrowserTool{DataDir: t.TempDir(), Headless: true}
	ctx := WithSessionInfo(context.Background(), "test-session", "default")

	input, _ := json.Marshal(browserInput{Action: "navigate"})
	_, err := bt.Execute(ctx, input, nil, sandbox.Policy{})
	if err == nil {
		t.Error("expected error for navigate without url")
	}
}

func TestBrowserTool_ClickRequiresSelector(t *testing.T) {
	bt := &BrowserTool{DataDir: t.TempDir(), Headless: true}
	ctx := WithSessionInfo(context.Background(), "test-session", "default")

	input, _ := json.Marshal(browserInput{Action: "click"})
	_, err := bt.Execute(ctx, input, nil, sandbox.Policy{})
	if err == nil {
		t.Error("expected error for click without selector")
	}
}

func TestBrowserTool_TypeRequiresFields(t *testing.T) {
	bt := &BrowserTool{DataDir: t.TempDir(), Headless: true}
	ctx := WithSessionInfo(context.Background(), "test-session", "default")

	input, _ := json.Marshal(browserInput{Action: "type"})
	_, err := bt.Execute(ctx, input, nil, sandbox.Policy{})
	if err == nil {
		t.Error("expected error for type without selector")
	}

	input, _ = json.Marshal(browserInput{Action: "type", Selector: "input"})
	_, err = bt.Execute(ctx, input, nil, sandbox.Policy{})
	if err == nil {
		t.Error("expected error for type without text")
	}
}

func TestBrowserTool_WaitRequiresSelector(t *testing.T) {
	bt := &BrowserTool{DataDir: t.TempDir(), Headless: true}
	ctx := WithSessionInfo(context.Background(), "test-session", "default")

	input, _ := json.Marshal(browserInput{Action: "wait"})
	_, err := bt.Execute(ctx, input, nil, sandbox.Policy{})
	if err == nil {
		t.Error("expected error for wait without selector")
	}
}

func TestBrowserTool_EvaluateRequiresScript(t *testing.T) {
	bt := &BrowserTool{DataDir: t.TempDir(), Headless: true}
	ctx := WithSessionInfo(context.Background(), "test-session", "default")

	input, _ := json.Marshal(browserInput{Action: "evaluate"})
	_, err := bt.Execute(ctx, input, nil, sandbox.Policy{})
	if err == nil {
		t.Error("expected error for evaluate without script")
	}
}

func TestBrowserTool_SelectRequiresFields(t *testing.T) {
	bt := &BrowserTool{DataDir: t.TempDir(), Headless: true}
	ctx := WithSessionInfo(context.Background(), "test-session", "default")

	input, _ := json.Marshal(browserInput{Action: "select"})
	_, err := bt.Execute(ctx, input, nil, sandbox.Policy{})
	if err == nil {
		t.Error("expected error for select without selector")
	}

	input, _ = json.Marshal(browserInput{Action: "select", Selector: "select"})
	_, err = bt.Execute(ctx, input, nil, sandbox.Policy{})
	if err == nil {
		t.Error("expected error for select without text")
	}
}

func TestBrowserTool_ConsumeImages_Empty(t *testing.T) {
	bt := &BrowserTool{
		pendingImages: make(map[string][]llm.ImageContent),
	}
	ctx := WithSessionInfo(context.Background(), "test-session", "default")

	images := bt.ConsumeImages(ctx)
	if images != nil {
		t.Errorf("expected nil, got %d images", len(images))
	}
}

func TestBrowserTool_ConsumeImages_NoSession(t *testing.T) {
	bt := &BrowserTool{
		pendingImages: make(map[string][]llm.ImageContent),
	}

	images := bt.ConsumeImages(context.Background())
	if images != nil {
		t.Error("expected nil when no session in context")
	}
}

func TestBrowserTool_ConsumeImages_ReturnsAndClears(t *testing.T) {
	pending := []llm.ImageContent{
		{MediaType: "image/png", Path: "/tmp/test.png"},
	}
	bt := &BrowserTool{
		pendingImages: map[string][]llm.ImageContent{
			"sess1": pending,
		},
	}
	ctx := WithSessionInfo(context.Background(), "sess1", "default")

	images := bt.ConsumeImages(ctx)
	if len(images) != 1 {
		t.Fatalf("expected 1 image, got %d", len(images))
	}
	if images[0].MediaType != "image/png" {
		t.Errorf("MediaType = %q, want %q", images[0].MediaType, "image/png")
	}

	images2 := bt.ConsumeImages(ctx)
	if images2 != nil {
		t.Error("expected nil after consuming, images should be cleared")
	}
}

func TestBrowserTool_CloseNoSession(t *testing.T) {
	bt := &BrowserTool{
		sessions:      make(map[string]*browserSession),
		pendingImages: make(map[string][]llm.ImageContent),
	}
	ctx := WithSessionInfo(context.Background(), "nonexistent", "default")

	input, _ := json.Marshal(browserInput{Action: "close"})
	result, err := bt.Execute(ctx, input, nil, sandbox.Policy{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "no browser session to close" {
		t.Errorf("result = %q, want %q", result, "no browser session to close")
	}
}

func TestBrowserTool_CleanIdleSessions(t *testing.T) {
	bt := &BrowserTool{
		sessions:      make(map[string]*browserSession),
		pendingImages: make(map[string][]llm.ImageContent),
	}

	bt.sessions["old"] = &browserSession{
		lastUsed:    time.Now().Add(-20 * time.Minute),
		ctxCancel:   func() {},
		allocCancel: func() {},
	}
	bt.sessions["recent"] = &browserSession{
		lastUsed:    time.Now(),
		ctxCancel:   func() {},
		allocCancel: func() {},
	}

	bt.cleanIdleSessions(10 * time.Minute)

	if _, ok := bt.sessions["old"]; ok {
		t.Error("expected old session to be cleaned up")
	}
	if _, ok := bt.sessions["recent"]; !ok {
		t.Error("expected recent session to remain")
	}
}

func TestBrowserTool_ImageProducerInterface(t *testing.T) {
	var _ ImageProducer = &BrowserTool{}
}

func TestBrowserTool_AuditLogNilSafe(t *testing.T) {
	bt := &BrowserTool{}
	bt.auditLog(context.Background(), "test", "sess", "detail")
}

func TestBrowserTool_StartCleanupAndClose(t *testing.T) {
	bt := &BrowserTool{DataDir: t.TempDir(), Headless: true}
	bt.StartCleanup()

	if bt.sessions == nil {
		t.Error("sessions map should be initialized after StartCleanup")
	}
	if bt.pendingImages == nil {
		t.Error("pendingImages map should be initialized after StartCleanup")
	}

	bt.Close()
}

func skipIfNoChrome(t *testing.T) {
	t.Helper()
	paths := []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser"}
	for _, p := range paths {
		if _, err := exec.LookPath(p); err == nil {
			return
		}
	}
	if _, err := exec.LookPath("/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"); err == nil {
		return
	}
	t.Skip("no Chrome/Chromium found, skipping integration test")
}

func TestBrowserTool_NavigateIntegration(t *testing.T) {
	skipIfNoChrome(t)

	bt := &BrowserTool{
		DataDir:     t.TempDir(),
		Headless:    true,
		IdleTimeout: time.Minute,
	}
	bt.StartCleanup()
	defer bt.Close()

	ctx := WithSessionInfo(context.Background(), "integration-test", "default")

	input, _ := json.Marshal(browserInput{Action: "navigate", URL: "about:blank"})
	result, err := bt.Execute(ctx, input, nil, sandbox.Policy{})
	if err != nil {
		t.Fatalf("navigate failed: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty result from navigate")
	}

	images := bt.ConsumeImages(ctx)
	if len(images) != 1 {
		t.Errorf("expected 1 screenshot, got %d", len(images))
	}

	input, _ = json.Marshal(browserInput{Action: "screenshot"})
	result, err = bt.Execute(ctx, input, nil, sandbox.Policy{})
	if err != nil {
		t.Fatalf("screenshot failed: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty result from screenshot")
	}

	input, _ = json.Marshal(browserInput{Action: "evaluate", Script: "document.title"})
	result, err = bt.Execute(ctx, input, nil, sandbox.Policy{})
	if err != nil {
		t.Fatalf("evaluate failed: %v", err)
	}

	input, _ = json.Marshal(browserInput{Action: "close"})
	result, err = bt.Execute(ctx, input, nil, sandbox.Policy{})
	if err != nil {
		t.Fatalf("close failed: %v", err)
	}
	if result != "browser session closed" {
		t.Errorf("result = %q, want %q", result, "browser session closed")
	}
}

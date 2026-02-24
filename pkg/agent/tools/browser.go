package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/igorsilveira/pincer/pkg/llm"
	"github.com/igorsilveira/pincer/pkg/sandbox"
)

type ImageProducer interface {
	ConsumeImages(ctx context.Context) []llm.ImageContent
}

type BrowserTool struct {
	DataDir     string
	Headless    bool
	IdleTimeout time.Duration
	AuditLog    func(ctx context.Context, eventType, sessionID, detail string)

	mu            sync.Mutex
	sessions      map[string]*browserSession
	pendingImages map[string][]llm.ImageContent
	cleanupDone   chan struct{}
}

type browserSession struct {
	allocCancel context.CancelFunc
	ctxCancel   context.CancelFunc
	ctx         context.Context
	lastUsed    time.Time
}

type browserInput struct {
	Action   string `json:"action"`
	URL      string `json:"url,omitempty"`
	Selector string `json:"selector,omitempty"`
	Text     string `json:"text,omitempty"`
	Script   string `json:"script,omitempty"`
	Delta    int    `json:"delta,omitempty"`
}

func (t *BrowserTool) Definition() llm.ToolDefinition {
	return llm.ToolDefinition{
		Name:        "browser",
		Description: "Control a headless browser to navigate pages, interact with elements, and take screenshots. After most actions a screenshot is automatically captured so you can see the result.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"action": {
					"type": "string",
					"enum": ["navigate", "click", "type", "screenshot", "get_text", "get_html", "wait", "evaluate", "scroll", "select", "back", "forward", "close"],
					"description": "The browser action to perform."
				},
				"url": {
					"type": "string",
					"description": "URL to navigate to. Required for navigate."
				},
				"selector": {
					"type": "string",
					"description": "CSS selector for the target element. Required for click, type, wait, select. Optional for get_text, get_html."
				},
				"text": {
					"type": "string",
					"description": "Text to type into an element (for type action) or option text to select (for select action)."
				},
				"script": {
					"type": "string",
					"description": "JavaScript code to evaluate in the page (for evaluate action)."
				},
				"delta": {
					"type": "integer",
					"description": "Pixels to scroll vertically. Positive scrolls down, negative scrolls up. Default 500."
				}
			},
			"required": ["action"]
		}`),
	}
}

func (t *BrowserTool) Execute(ctx context.Context, input json.RawMessage, _ sandbox.Sandbox, _ sandbox.Policy) (string, error) {
	params, err := parseInput[browserInput](input, "browser")
	if err != nil {
		return "", err
	}

	sessionID := SessionIDFromContext(ctx)
	if sessionID == "" {
		return "", fmt.Errorf("browser: no session in context")
	}

	slog.Debug("browser action started",
		slog.String("action", params.Action),
		slog.String("session_id", sessionID),
	)
	actionStart := time.Now()
	defer func() {
		slog.Info("browser action completed",
			slog.String("action", params.Action),
			slog.String("session_id", sessionID),
			slog.Duration("duration", time.Since(actionStart)),
		)
	}()

	switch params.Action {
	case "navigate":
		return t.doNavigate(ctx, sessionID, params)
	case "click":
		return t.doClick(ctx, sessionID, params)
	case "type":
		return t.doType(ctx, sessionID, params)
	case "screenshot":
		return t.doScreenshot(ctx, sessionID)
	case "get_text":
		return t.doGetText(ctx, sessionID, params)
	case "get_html":
		return t.doGetHTML(ctx, sessionID, params)
	case "wait":
		return t.doWait(ctx, sessionID, params)
	case "evaluate":
		return t.doEvaluate(ctx, sessionID, params)
	case "scroll":
		return t.doScroll(ctx, sessionID, params)
	case "select":
		return t.doSelect(ctx, sessionID, params)
	case "back":
		return t.doBack(ctx, sessionID)
	case "forward":
		return t.doForward(ctx, sessionID)
	case "close":
		return t.doClose(ctx, sessionID)
	default:
		return "", fmt.Errorf("browser: unknown action %q", params.Action)
	}
}

func (t *BrowserTool) ConsumeImages(ctx context.Context) []llm.ImageContent {
	sessionID := SessionIDFromContext(ctx)
	if sessionID == "" {
		return nil
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	images := t.pendingImages[sessionID]
	delete(t.pendingImages, sessionID)
	return images
}

func (t *BrowserTool) StartCleanup() {
	t.mu.Lock()
	if t.sessions == nil {
		t.sessions = make(map[string]*browserSession)
	}
	if t.pendingImages == nil {
		t.pendingImages = make(map[string][]llm.ImageContent)
	}
	t.cleanupDone = make(chan struct{})
	t.mu.Unlock()

	timeout := t.IdleTimeout
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}

	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				t.cleanIdleSessions(timeout)
			case <-t.cleanupDone:
				return
			}
		}
	}()
}

func (t *BrowserTool) Close() {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.cleanupDone != nil {
		close(t.cleanupDone)
		t.cleanupDone = nil
	}

	slog.Info("browser closing all sessions", slog.Int("count", len(t.sessions)))

	for id, sess := range t.sessions {
		sess.ctxCancel()
		sess.allocCancel()
		delete(t.sessions, id)
	}
}

func (t *BrowserTool) cleanIdleSessions(timeout time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()
	for id, sess := range t.sessions {
		if now.Sub(sess.lastUsed) > timeout {
			slog.Info("browser session evicted (idle)",
				slog.String("session_id", id),
				slog.Duration("idle_time", now.Sub(sess.lastUsed)),
			)
			sess.ctxCancel()
			sess.allocCancel()
			delete(t.sessions, id)
		}
	}
}

func (t *BrowserTool) getOrCreateSession(sessionID string) (context.Context, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.sessions == nil {
		t.sessions = make(map[string]*browserSession)
	}
	if t.pendingImages == nil {
		t.pendingImages = make(map[string][]llm.ImageContent)
	}

	if sess, ok := t.sessions[sessionID]; ok {
		sess.lastUsed = time.Now()
		return sess.ctx, nil
	}

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", t.Headless),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.WindowSize(1280, 720),
	)

	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	taskCtx, taskCancel := chromedp.NewContext(allocCtx)

	if err := chromedp.Run(taskCtx); err != nil {
		taskCancel()
		allocCancel()
		return nil, fmt.Errorf("browser: starting chrome: %w", err)
	}

	t.sessions[sessionID] = &browserSession{
		allocCancel: allocCancel,
		ctxCancel:   taskCancel,
		ctx:         taskCtx,
		lastUsed:    time.Now(),
	}

	slog.Info("browser session created", slog.String("session_id", sessionID))

	return taskCtx, nil
}

func (t *BrowserTool) captureScreenshot(browserCtx context.Context, sessionID string) (string, error) {
	var buf []byte
	if err := chromedp.Run(browserCtx, chromedp.FullScreenshot(&buf, 100)); err != nil {
		return "", fmt.Errorf("browser: screenshot failed: %w", err)
	}

	dir := filepath.Join(t.DataDir, "screenshots", sessionID)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("browser: creating screenshot dir: %w", err)
	}

	filename := fmt.Sprintf("%d.png", time.Now().UnixMilli())
	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, buf, 0600); err != nil {
		return "", fmt.Errorf("browser: writing screenshot: %w", err)
	}

	t.mu.Lock()
	t.pendingImages[sessionID] = append(t.pendingImages[sessionID], llm.ImageContent{
		MediaType: "image/png",
		Path:      path,
	})
	t.pendingImages[sessionID][len(t.pendingImages[sessionID])-1].SetData(buf)
	t.mu.Unlock()

	slog.Debug("browser screenshot captured",
		slog.String("session_id", sessionID),
		slog.String("path", path),
		slog.Int("size_bytes", len(buf)),
	)

	return path, nil
}

func (t *BrowserTool) pageInfo(browserCtx context.Context) (title, url string) {
	_ = chromedp.Run(browserCtx,
		chromedp.Title(&title),
		chromedp.Location(&url),
	)
	return
}

func (t *BrowserTool) doNavigate(ctx context.Context, sessionID string, params browserInput) (string, error) {
	if params.URL == "" {
		return "", fmt.Errorf("browser: url is required for navigate")
	}

	browserCtx, err := t.getOrCreateSession(sessionID)
	if err != nil {
		return "", err
	}

	if err := chromedp.Run(browserCtx, chromedp.Navigate(params.URL)); err != nil {
		return "", fmt.Errorf("browser: navigate failed: %w", err)
	}

	t.auditLog(ctx, "browser_nav", sessionID, params.URL)

	title, loc := t.pageInfo(browserCtx)
	path, _ := t.captureScreenshot(browserCtx, sessionID)
	return fmt.Sprintf("Navigated to %s\nTitle: %s\nScreenshot: %s", loc, title, path), nil
}

func (t *BrowserTool) doClick(ctx context.Context, sessionID string, params browserInput) (string, error) {
	if params.Selector == "" {
		return "", fmt.Errorf("browser: selector is required for click")
	}

	browserCtx, err := t.getOrCreateSession(sessionID)
	if err != nil {
		return "", err
	}

	if err := chromedp.Run(browserCtx,
		chromedp.WaitVisible(params.Selector, chromedp.ByQuery),
		chromedp.Click(params.Selector, chromedp.ByQuery),
	); err != nil {
		return "", fmt.Errorf("browser: click failed: %w", err)
	}

	_ = chromedp.Run(browserCtx, chromedp.Sleep(500*time.Millisecond))

	title, loc := t.pageInfo(browserCtx)
	path, _ := t.captureScreenshot(browserCtx, sessionID)
	return fmt.Sprintf("Clicked %q\nPage: %s (%s)\nScreenshot: %s", params.Selector, loc, title, path), nil
}

func (t *BrowserTool) doType(_ context.Context, sessionID string, params browserInput) (string, error) {
	if params.Selector == "" {
		return "", fmt.Errorf("browser: selector is required for type")
	}
	if params.Text == "" {
		return "", fmt.Errorf("browser: text is required for type")
	}

	browserCtx, err := t.getOrCreateSession(sessionID)
	if err != nil {
		return "", err
	}

	if err := chromedp.Run(browserCtx,
		chromedp.WaitVisible(params.Selector, chromedp.ByQuery),
		chromedp.Clear(params.Selector, chromedp.ByQuery),
		chromedp.SendKeys(params.Selector, params.Text, chromedp.ByQuery),
	); err != nil {
		return "", fmt.Errorf("browser: type failed: %w", err)
	}

	path, _ := t.captureScreenshot(browserCtx, sessionID)
	return fmt.Sprintf("Typed %d chars into %q\nScreenshot: %s", len(params.Text), params.Selector, path), nil
}

func (t *BrowserTool) doScreenshot(_ context.Context, sessionID string) (string, error) {
	browserCtx, err := t.getOrCreateSession(sessionID)
	if err != nil {
		return "", err
	}

	title, loc := t.pageInfo(browserCtx)
	path, err := t.captureScreenshot(browserCtx, sessionID)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Screenshot captured\nPage: %s (%s)\nPath: %s", loc, title, path), nil
}

func (t *BrowserTool) doGetText(_ context.Context, sessionID string, params browserInput) (string, error) {
	browserCtx, err := t.getOrCreateSession(sessionID)
	if err != nil {
		return "", err
	}

	var text string
	sel := params.Selector
	if sel == "" {
		sel = "body"
	}

	if err := chromedp.Run(browserCtx,
		chromedp.Text(sel, &text, chromedp.ByQuery),
	); err != nil {
		return "", fmt.Errorf("browser: get_text failed: %w", err)
	}

	const maxLen = 50000
	if len(text) > maxLen {
		text = text[:maxLen] + "\n... (truncated)"
	}

	return text, nil
}

func (t *BrowserTool) doGetHTML(_ context.Context, sessionID string, params browserInput) (string, error) {
	browserCtx, err := t.getOrCreateSession(sessionID)
	if err != nil {
		return "", err
	}

	var html string
	sel := params.Selector
	if sel == "" {
		sel = "html"
	}

	if err := chromedp.Run(browserCtx,
		chromedp.OuterHTML(sel, &html, chromedp.ByQuery),
	); err != nil {
		return "", fmt.Errorf("browser: get_html failed: %w", err)
	}

	const maxLen = 50000
	if len(html) > maxLen {
		html = html[:maxLen] + "\n... (truncated)"
	}

	return html, nil
}

func (t *BrowserTool) doWait(_ context.Context, sessionID string, params browserInput) (string, error) {
	if params.Selector == "" {
		return "", fmt.Errorf("browser: selector is required for wait")
	}

	browserCtx, err := t.getOrCreateSession(sessionID)
	if err != nil {
		return "", err
	}

	if err := chromedp.Run(browserCtx,
		chromedp.WaitVisible(params.Selector, chromedp.ByQuery),
	); err != nil {
		return "", fmt.Errorf("browser: wait failed: %w", err)
	}

	return fmt.Sprintf("Element %q is now visible", params.Selector), nil
}

func (t *BrowserTool) doEvaluate(_ context.Context, sessionID string, params browserInput) (string, error) {
	if params.Script == "" {
		return "", fmt.Errorf("browser: script is required for evaluate")
	}

	browserCtx, err := t.getOrCreateSession(sessionID)
	if err != nil {
		return "", err
	}

	var result interface{}
	if err := chromedp.Run(browserCtx,
		chromedp.Evaluate(params.Script, &result),
	); err != nil {
		return "", fmt.Errorf("browser: evaluate failed: %w", err)
	}

	b, _ := json.Marshal(result)
	return string(b), nil
}

func (t *BrowserTool) doScroll(_ context.Context, sessionID string, params browserInput) (string, error) {
	browserCtx, err := t.getOrCreateSession(sessionID)
	if err != nil {
		return "", err
	}

	delta := params.Delta
	if delta == 0 {
		delta = 500
	}

	script := fmt.Sprintf("window.scrollBy(0, %d)", delta)
	if err := chromedp.Run(browserCtx,
		chromedp.Evaluate(script, nil),
		chromedp.Sleep(300*time.Millisecond),
	); err != nil {
		return "", fmt.Errorf("browser: scroll failed: %w", err)
	}

	direction := "down"
	if delta < 0 {
		direction = "up"
	}

	path, _ := t.captureScreenshot(browserCtx, sessionID)
	return fmt.Sprintf("Scrolled %s by %dpx\nScreenshot: %s", direction, abs(delta), path), nil
}

func (t *BrowserTool) doSelect(_ context.Context, sessionID string, params browserInput) (string, error) {
	if params.Selector == "" {
		return "", fmt.Errorf("browser: selector is required for select")
	}
	if params.Text == "" {
		return "", fmt.Errorf("browser: text is required for select")
	}

	browserCtx, err := t.getOrCreateSession(sessionID)
	if err != nil {
		return "", err
	}

	script := fmt.Sprintf(`(function() {
		var sel = document.querySelector(%q);
		if (!sel) return "element not found";
		for (var i = 0; i < sel.options.length; i++) {
			if (sel.options[i].text === %q || sel.options[i].value === %q) {
				sel.selectedIndex = i;
				sel.dispatchEvent(new Event('change', {bubbles: true}));
				return "selected: " + sel.options[i].text;
			}
		}
		return "option not found";
	})()`, params.Selector, params.Text, params.Text)

	var result string
	if err := chromedp.Run(browserCtx,
		chromedp.Evaluate(script, &result),
	); err != nil {
		return "", fmt.Errorf("browser: select failed: %w", err)
	}

	if strings.HasPrefix(result, "element not found") || strings.HasPrefix(result, "option not found") {
		return "", fmt.Errorf("browser: %s", result)
	}

	path, _ := t.captureScreenshot(browserCtx, sessionID)
	return fmt.Sprintf("Selected %q in %q\nScreenshot: %s", params.Text, params.Selector, path), nil
}

func (t *BrowserTool) doBack(_ context.Context, sessionID string) (string, error) {
	browserCtx, err := t.getOrCreateSession(sessionID)
	if err != nil {
		return "", err
	}

	if err := chromedp.Run(browserCtx, chromedp.NavigateBack()); err != nil {
		return "", fmt.Errorf("browser: back failed: %w", err)
	}

	_ = chromedp.Run(browserCtx, chromedp.Sleep(500*time.Millisecond))

	title, loc := t.pageInfo(browserCtx)
	path, _ := t.captureScreenshot(browserCtx, sessionID)
	return fmt.Sprintf("Navigated back\nPage: %s (%s)\nScreenshot: %s", loc, title, path), nil
}

func (t *BrowserTool) doForward(_ context.Context, sessionID string) (string, error) {
	browserCtx, err := t.getOrCreateSession(sessionID)
	if err != nil {
		return "", err
	}

	if err := chromedp.Run(browserCtx, chromedp.NavigateForward()); err != nil {
		return "", fmt.Errorf("browser: forward failed: %w", err)
	}

	_ = chromedp.Run(browserCtx, chromedp.Sleep(500*time.Millisecond))

	title, loc := t.pageInfo(browserCtx)
	path, _ := t.captureScreenshot(browserCtx, sessionID)
	return fmt.Sprintf("Navigated forward\nPage: %s (%s)\nScreenshot: %s", loc, title, path), nil
}

func (t *BrowserTool) doClose(ctx context.Context, sessionID string) (string, error) {
	t.mu.Lock()
	sess, ok := t.sessions[sessionID]
	if ok {
		sess.ctxCancel()
		sess.allocCancel()
		delete(t.sessions, sessionID)
	}
	t.mu.Unlock()

	if !ok {
		return "no browser session to close", nil
	}

	t.auditLog(ctx, "browser_close", sessionID, "")
	return "browser session closed", nil
}

func (t *BrowserTool) auditLog(ctx context.Context, eventType, sessionID, detail string) {
	if t.AuditLog != nil {
		t.AuditLog(ctx, eventType, sessionID, detail)
	}
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

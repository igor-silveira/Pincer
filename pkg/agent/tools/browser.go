package tools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/igorsilveira/pincer/pkg/llm"
	"github.com/igorsilveira/pincer/pkg/sandbox"
)

type BrowserTool struct{}

type browserInput struct {
	Action   string `json:"action"`
	URL      string `json:"url,omitempty"`
	Selector string `json:"selector,omitempty"`
	Text     string `json:"text,omitempty"`
	Wait     int    `json:"wait_ms,omitempty"`
}

func (t *BrowserTool) Definition() llm.ToolDefinition {
	return llm.ToolDefinition{
		Name:        "browser",
		Description: "Control a headless browser. Actions: navigate, screenshot, click, type, extract, eval.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"action": {
					"type": "string",
					"enum": ["navigate", "screenshot", "click", "type", "extract", "eval"],
					"description": "The browser action to perform"
				},
				"url": {
					"type": "string",
					"description": "URL to navigate to (for navigate action)"
				},
				"selector": {
					"type": "string",
					"description": "CSS selector for the target element"
				},
				"text": {
					"type": "string",
					"description": "Text to type or JavaScript to evaluate"
				},
				"wait_ms": {
					"type": "integer",
					"description": "Milliseconds to wait after the action (default 1000)"
				}
			},
			"required": ["action"]
		}`),
	}
}

func (t *BrowserTool) Execute(ctx context.Context, input json.RawMessage, _ sandbox.Sandbox, policy sandbox.Policy) (string, error) {
	in, err := parseInput[browserInput](input, "browser")
	if err != nil {
		return "", err
	}

	if policy.NetworkAccess == sandbox.NetworkDeny {
		return "", fmt.Errorf("browser: network access denied by sandbox policy")
	}

	timeout := policy.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	allocCtx, allocCancel := chromedp.NewExecAllocator(ctx,
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("disable-dev-shm-usage", true),
	)
	defer allocCancel()

	taskCtx, taskCancel := chromedp.NewContext(allocCtx)
	defer taskCancel()

	taskCtx, timeoutCancel := context.WithTimeout(taskCtx, timeout)
	defer timeoutCancel()

	waitDuration := time.Duration(in.Wait) * time.Millisecond
	if waitDuration <= 0 {
		waitDuration = time.Second
	}

	switch in.Action {
	case "navigate":
		return t.navigate(taskCtx, in.URL, waitDuration)
	case "screenshot":
		return t.screenshot(taskCtx, in.Selector)
	case "click":
		return t.click(taskCtx, in.Selector, waitDuration)
	case "type":
		return t.typeText(taskCtx, in.Selector, in.Text, waitDuration)
	case "extract":
		return t.extract(taskCtx, in.Selector)
	case "eval":
		return t.eval(taskCtx, in.Text)
	default:
		return "", fmt.Errorf("browser: unknown action %q", in.Action)
	}
}

func (t *BrowserTool) navigate(ctx context.Context, url string, wait time.Duration) (string, error) {
	if url == "" {
		return "", fmt.Errorf("browser: url is required for navigate")
	}

	var title string
	err := chromedp.Run(ctx,
		chromedp.Navigate(url),
		chromedp.Sleep(wait),
		chromedp.Title(&title),
	)
	if err != nil {
		return "", fmt.Errorf("browser: navigate: %w", err)
	}

	return fmt.Sprintf("Navigated to %s (title: %q)", url, title), nil
}

func (t *BrowserTool) screenshot(ctx context.Context, selector string) (string, error) {
	var buf []byte
	var err error

	if selector != "" {
		err = chromedp.Run(ctx, chromedp.Screenshot(selector, &buf, chromedp.NodeVisible))
	} else {
		err = chromedp.Run(ctx, chromedp.FullScreenshot(&buf, 90))
	}
	if err != nil {
		return "", fmt.Errorf("browser: screenshot: %w", err)
	}

	encoded := base64.StdEncoding.EncodeToString(buf)

	if len(encoded) > 10000 {
		return fmt.Sprintf("Screenshot captured (%d bytes). Base64 prefix: %s...", len(buf), encoded[:200]), nil
	}
	return fmt.Sprintf("Screenshot captured (%d bytes): data:image/png;base64,%s", len(buf), encoded), nil
}

func (t *BrowserTool) click(ctx context.Context, selector string, wait time.Duration) (string, error) {
	if selector == "" {
		return "", fmt.Errorf("browser: selector is required for click")
	}

	err := chromedp.Run(ctx,
		chromedp.Click(selector, chromedp.ByQuery),
		chromedp.Sleep(wait),
	)
	if err != nil {
		return "", fmt.Errorf("browser: click: %w", err)
	}

	return fmt.Sprintf("Clicked element %q", selector), nil
}

func (t *BrowserTool) typeText(ctx context.Context, selector, text string, wait time.Duration) (string, error) {
	if selector == "" {
		return "", fmt.Errorf("browser: selector is required for type")
	}

	err := chromedp.Run(ctx,
		chromedp.SendKeys(selector, text, chromedp.ByQuery),
		chromedp.Sleep(wait),
	)
	if err != nil {
		return "", fmt.Errorf("browser: type: %w", err)
	}

	return fmt.Sprintf("Typed %d characters into %q", len(text), selector), nil
}

func (t *BrowserTool) extract(ctx context.Context, selector string) (string, error) {
	if selector == "" {

		var text string
		err := chromedp.Run(ctx, chromedp.InnerHTML("body", &text, chromedp.ByQuery))
		if err != nil {
			return "", fmt.Errorf("browser: extract body: %w", err)
		}
		if len(text) > 50000 {
			text = text[:50000] + "\n... (truncated)"
		}
		return text, nil
	}

	var nodes []*string
	var results []string
	err := chromedp.Run(ctx, chromedp.EvaluateAsDevTools(
		fmt.Sprintf(`Array.from(document.querySelectorAll(%q)).map(el => el.textContent.trim())`, selector),
		&nodes,
	))
	if err != nil {

		var text string
		err = chromedp.Run(ctx, chromedp.Text(selector, &text, chromedp.ByQuery))
		if err != nil {
			return "", fmt.Errorf("browser: extract: %w", err)
		}
		return text, nil
	}

	for _, n := range nodes {
		if n != nil {
			results = append(results, *n)
		}
	}
	return strings.Join(results, "\n"), nil
}

func (t *BrowserTool) eval(ctx context.Context, expression string) (string, error) {
	if expression == "" {
		return "", fmt.Errorf("browser: text is required for eval (JavaScript expression)")
	}

	var result interface{}
	err := chromedp.Run(ctx, chromedp.Evaluate(expression, &result))
	if err != nil {
		return "", fmt.Errorf("browser: eval: %w", err)
	}

	b, _ := json.Marshal(result)
	return string(b), nil
}

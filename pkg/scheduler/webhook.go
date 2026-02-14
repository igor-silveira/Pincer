package scheduler

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"

	"github.com/igorsilveira/pincer/pkg/telemetry"
)

type WebhookPayload struct {
	Event   string          `json:"event"`
	Source  string          `json:"source"`
	Payload json.RawMessage `json:"payload"`
}

type WebhookHandler struct {
	secret   string
	handlers map[string]WebhookFunc
	mu       sync.RWMutex
}

type WebhookFunc func(ctx context.Context, payload WebhookPayload) error

func NewWebhookHandler(secret string) *WebhookHandler {
	return &WebhookHandler{
		secret:   secret,
		handlers: make(map[string]WebhookFunc),
	}
}

func (wh *WebhookHandler) On(event string, fn WebhookFunc) {
	wh.mu.Lock()
	defer wh.mu.Unlock()
	wh.handlers[event] = fn
}

func (wh *WebhookHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	if wh.secret != "" {
		sig := r.Header.Get("X-Pincer-Signature")
		if !wh.verifySignature(body, sig) {
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return
		}
	}

	var payload WebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	logger := telemetry.FromContext(r.Context())
	logger.Info("webhook received",
		slog.String("event", payload.Event),
		slog.String("source", payload.Source),
	)

	wh.mu.RLock()
	handler, ok := wh.handlers[payload.Event]
	wh.mu.RUnlock()

	if !ok {

		w.WriteHeader(http.StatusAccepted)
		fmt.Fprint(w, `{"status":"accepted","handled":false}`)
		return
	}

	if err := handler(r.Context(), payload); err != nil {
		logger.Error("webhook handler failed",
			slog.String("event", payload.Event),
			slog.String("err", err.Error()),
		)
		http.Error(w, "handler error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, `{"status":"ok","handled":true}`)
}

func (wh *WebhookHandler) verifySignature(body []byte, signature string) bool {
	if signature == "" {
		return false
	}

	mac := hmac.New(sha256.New, []byte(wh.secret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(expected), []byte(signature))
}

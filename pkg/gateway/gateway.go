package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/igorsilveira/pincer/pkg/agent"
	"github.com/igorsilveira/pincer/pkg/channels/webchat"
	"github.com/igorsilveira/pincer/pkg/telemetry"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Gateway struct {
	server     *http.Server
	router     *chi.Mux
	runtime    *agent.Runtime
	chat       *webchat.Adapter
	approver   *agent.Approver
	logger     *slog.Logger
	webhooks   http.Handler
	a2aHandler http.Handler
	authToken  string
}

type Config struct {
	Bind       string
	Port       int
	Runtime    *agent.Runtime
	Chat       *webchat.Adapter
	Approver   *agent.Approver
	Logger     *slog.Logger
	Webhooks   http.Handler
	A2AHandler http.Handler
	AuthToken  string
}

func New(cfg Config) *Gateway {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)

	g := &Gateway{
		router:     r,
		runtime:    cfg.Runtime,
		chat:       cfg.Chat,
		approver:   cfg.Approver,
		logger:     cfg.Logger,
		webhooks:   cfg.Webhooks,
		a2aHandler: cfg.A2AHandler,
		authToken:  cfg.AuthToken,
	}

	g.registerRoutes()

	addr := resolveAddr(cfg.Bind, cfg.Port)
	g.server = &http.Server{
		Addr:              addr,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	return g
}

func (g *Gateway) registerRoutes() {
	g.router.Get("/healthz", g.handleHealthz)
	g.router.Get("/readyz", g.handleReadyz)
	g.router.Handle("/metrics", promhttp.Handler())

	if g.a2aHandler != nil {
		g.router.Mount("/", g.a2aHandler)
	}

	g.router.Group(func(r chi.Router) {
		if g.authToken != "" {
			r.Use(g.authMiddleware)
		}
		r.Get("/", g.handleWebChatPage)
		r.Get("/ws", g.handleWebSocket)
		if g.webhooks != nil {
			r.Post("/webhooks", g.webhooks.ServeHTTP)
		}
	})
}

func (g *Gateway) Start(ctx context.Context) error {
	logger := telemetry.FromContext(ctx)
	logger.Info("gateway listening", slog.String("addr", g.server.Addr))

	ln, err := net.Listen("tcp", g.server.Addr)
	if err != nil {
		return fmt.Errorf("gateway listen: %w", err)
	}

	errCh := make(chan error, 1)
	go func() {
		if err := g.server.Serve(ln); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		return g.shutdown()
	case err := <-errCh:
		return err
	}
}

func (g *Gateway) shutdown() error {
	g.logger.Info("gateway shutting down")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return g.server.Shutdown(ctx)
}

func (g *Gateway) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, `{"status":"ok"}`)
}

func (g *Gateway) handleReadyz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, `{"status":"ready"}`)
}

func (g *Gateway) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		token := strings.TrimPrefix(header, "Bearer ")
		if token == "" || token == header || token != g.authToken {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func resolveAddr(bind string, port int) string {
	var host string
	switch bind {
	case "lan", "all":
		host = "0.0.0.0"
	case "loopback", "":
		host = "127.0.0.1"
	default:
		host = bind
	}
	return fmt.Sprintf("%s:%d", host, port)
}

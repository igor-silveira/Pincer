package gateway

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/igorsilveira/pincer/pkg/agent"
	"github.com/igorsilveira/pincer/pkg/channels/webchat"
	"github.com/igorsilveira/pincer/pkg/telemetry"
)

type Gateway struct {
	server  *http.Server
	router  *chi.Mux
	runtime *agent.Runtime
	chat    *webchat.Adapter
	logger  *slog.Logger
}

type Config struct {
	Bind    string
	Port    int
	Runtime *agent.Runtime
	Chat    *webchat.Adapter
	Logger  *slog.Logger
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
		router:  r,
		runtime: cfg.Runtime,
		chat:    cfg.Chat,
		logger:  cfg.Logger,
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
	g.router.Get("/", g.handleWebChatPage)
	g.router.Get("/ws", g.handleWebSocket)
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

func resolveAddr(bind string, port int) string {
	host := "127.0.0.1"
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

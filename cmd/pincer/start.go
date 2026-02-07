package pincer

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/igorsilveira/pincer/pkg/agent"
	"github.com/igorsilveira/pincer/pkg/agent/tools"
	"github.com/igorsilveira/pincer/pkg/channels/webchat"
	"github.com/igorsilveira/pincer/pkg/config"
	"github.com/igorsilveira/pincer/pkg/gateway"
	"github.com/igorsilveira/pincer/pkg/llm"
	"github.com/igorsilveira/pincer/pkg/sandbox"
	"github.com/igorsilveira/pincer/pkg/store"
	"github.com/igorsilveira/pincer/pkg/telemetry"
	"github.com/spf13/cobra"
)

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the Pincer gateway",
	RunE:  runStart,
}

func runStart(cmd *cobra.Command, args []string) error {
	path := cfgFile
	if path == "" {
		path = config.DefaultConfigPath()
	}

	cfg, err := config.Load(path)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	if err := config.EnsureDataDir(); err != nil {
		return fmt.Errorf("creating data directory: %w", err)
	}

	logger := telemetry.SetupLogger(cfg.Log.Level, cfg.Log.Format, nil)
	logger.Info("starting pincer gateway",
		slog.String("version", version),
		slog.Int("port", cfg.Gateway.Port),
		slog.String("bind", cfg.Gateway.Bind),
	)

	db, err := store.New(cfg.Store.DSN)
	if err != nil {
		return fmt.Errorf("opening store: %w", err)
	}
	defer db.Close()
	logger.Info("store ready", slog.String("dsn", cfg.Store.DSN))

	provider, err := llm.NewAnthropicProvider("")
	if err != nil {
		return fmt.Errorf("creating LLM provider: %w", err)
	}
	logger.Info("llm provider ready", slog.String("provider", provider.Name()))

	sb := sandbox.NewProcessSandbox(config.DataDir())
	registry := tools.DefaultRegistry()
	logger.Info("tool sandbox ready",
		slog.String("mode", cfg.Sandbox.Mode),
		slog.Int("tools", len(registry.Definitions())),
	)

	approvalMode := agent.ApprovalMode(cfg.Agent.ToolApproval)
	approver := agent.NewApprover(approvalMode, nil)

	runtime := agent.NewRuntime(agent.RuntimeConfig{
		Provider:     provider,
		Store:        db,
		Registry:     registry,
		Sandbox:      sb,
		Approver:     approver,
		Model:        cfg.Agent.Model,
		MaxTokens:    cfg.Agent.MaxContextTokens,
		SystemPrompt: cfg.Agent.SystemPrompt,
	})

	chat := webchat.New()

	gw := gateway.New(gateway.Config{
		Bind:     cfg.Gateway.Bind,
		Port:     cfg.Gateway.Port,
		Runtime:  runtime,
		Chat:     chat,
		Approver: approver,
		Logger:   logger,
	})

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	ctx = telemetry.WithLogger(ctx, logger)

	if err := chat.Start(ctx); err != nil {
		return fmt.Errorf("starting webchat adapter: %w", err)
	}

	logger.Info("pincer gateway ready",
		slog.String("url", fmt.Sprintf("http://127.0.0.1:%d", cfg.Gateway.Port)),
		slog.String("tool_approval", string(approvalMode)),
	)

	if err := gw.Start(ctx); err != nil {
		return fmt.Errorf("gateway error: %w", err)
	}

	logger.Info("pincer gateway stopped")
	return nil
}

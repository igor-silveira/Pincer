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
	"github.com/igorsilveira/pincer/pkg/channels"
	"github.com/igorsilveira/pincer/pkg/channels/discord"
	slackadapter "github.com/igorsilveira/pincer/pkg/channels/slack"
	"github.com/igorsilveira/pincer/pkg/channels/telegram"
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

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	ctx = telemetry.WithLogger(ctx, logger)

	chat := webchat.New()
	if err := chat.Start(ctx); err != nil {
		return fmt.Errorf("starting webchat adapter: %w", err)
	}

	var channelAdapters []channels.Adapter
	channelAdapters = append(channelAdapters, initChannelAdapters(ctx, cfg, logger)...)

	if len(channelAdapters) > 0 {
		router := gateway.NewChannelRouter(runtime, channelAdapters, logger)
		router.Start(ctx)
	}

	gw := gateway.New(gateway.Config{
		Bind:     cfg.Gateway.Bind,
		Port:     cfg.Gateway.Port,
		Runtime:  runtime,
		Chat:     chat,
		Approver: approver,
		Logger:   logger,
	})

	logger.Info("pincer gateway ready",
		slog.String("url", fmt.Sprintf("http://127.0.0.1:%d", cfg.Gateway.Port)),
		slog.String("tool_approval", string(approvalMode)),
		slog.Int("channel_adapters", len(channelAdapters)),
	)

	if err := gw.Start(ctx); err != nil {
		return fmt.Errorf("gateway error: %w", err)
	}

	logger.Info("pincer gateway stopped")
	return nil
}

func initChannelAdapters(ctx context.Context, cfg *config.Config, logger *slog.Logger) []channels.Adapter {
	var adapters []channels.Adapter

	if channelEnabled(cfg, "telegram") || os.Getenv("TELEGRAM_BOT_TOKEN") != "" {
		token := channelToken(cfg, "telegram")
		tg, err := telegram.New(token)
		if err != nil {
			logger.Warn("telegram adapter skipped", slog.String("err", err.Error()))
		} else if err := tg.Start(ctx); err != nil {
			logger.Error("telegram adapter failed to start", slog.String("err", err.Error()))
		} else {
			logger.Info("telegram adapter enabled")
			adapters = append(adapters, tg)
		}
	}

	if channelEnabled(cfg, "discord") || os.Getenv("DISCORD_BOT_TOKEN") != "" {
		token := channelToken(cfg, "discord")
		dc, err := discord.New(token)
		if err != nil {
			logger.Warn("discord adapter skipped", slog.String("err", err.Error()))
		} else if err := dc.Start(ctx); err != nil {
			logger.Error("discord adapter failed to start", slog.String("err", err.Error()))
		} else {
			logger.Info("discord adapter enabled")
			adapters = append(adapters, dc)
		}
	}

	if channelEnabled(cfg, "slack") || os.Getenv("SLACK_BOT_TOKEN") != "" {
		botToken := channelToken(cfg, "slack")
		appToken := os.Getenv("SLACK_APP_TOKEN")
		sl, err := slackadapter.New(botToken, appToken)
		if err != nil {
			logger.Warn("slack adapter skipped", slog.String("err", err.Error()))
		} else if err := sl.Start(ctx); err != nil {
			logger.Error("slack adapter failed to start", slog.String("err", err.Error()))
		} else {
			logger.Info("slack adapter enabled")
			adapters = append(adapters, sl)
		}
	}

	return adapters
}

func channelEnabled(cfg *config.Config, name string) bool {
	ch, ok := cfg.Channels[name]
	return ok && ch.Enabled
}

func channelToken(cfg *config.Config, name string) string {
	ch, ok := cfg.Channels[name]
	if !ok {
		return ""
	}
	if ch.Token != "" {
		return ch.Token
	}
	if ch.TokenEnv != "" {
		return os.Getenv(ch.TokenEnv)
	}
	return ""
}

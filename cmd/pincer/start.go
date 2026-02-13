package pincer

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/igorsilveira/pincer/pkg/agent"
	"github.com/igorsilveira/pincer/pkg/agent/tools"
	"github.com/igorsilveira/pincer/pkg/audit"
	"github.com/igorsilveira/pincer/pkg/channels"
	"github.com/igorsilveira/pincer/pkg/channels/discord"
	"github.com/igorsilveira/pincer/pkg/channels/matrix"
	slackadapter "github.com/igorsilveira/pincer/pkg/channels/slack"
	"github.com/igorsilveira/pincer/pkg/channels/telegram"
	"github.com/igorsilveira/pincer/pkg/channels/webchat"
	"github.com/igorsilveira/pincer/pkg/channels/whatsapp"
	"github.com/igorsilveira/pincer/pkg/config"
	"github.com/igorsilveira/pincer/pkg/credentials"
	"github.com/igorsilveira/pincer/pkg/gateway"
	"github.com/igorsilveira/pincer/pkg/llm"
	"github.com/igorsilveira/pincer/pkg/memory"
	"github.com/igorsilveira/pincer/pkg/sandbox"
	"github.com/igorsilveira/pincer/pkg/scheduler"
	"github.com/igorsilveira/pincer/pkg/skills"
	"github.com/igorsilveira/pincer/pkg/soul"
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

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	ctx = telemetry.WithLogger(ctx, logger)

	shutdownTracer, err := telemetry.InitTracer(ctx, telemetry.TracerConfig{
		Enabled:     cfg.Tracing.Enabled,
		Endpoint:    cfg.Tracing.Endpoint,
		ServiceName: "pincer",
		Version:     version,
	})
	if err != nil {
		return fmt.Errorf("initializing tracer: %w", err)
	}
	defer shutdownTracer(context.Background())
	if cfg.Tracing.Enabled {
		logger.Info("opentelemetry tracing enabled", slog.String("endpoint", cfg.Tracing.Endpoint))
	}

	db, err := store.New(cfg.Store.DSN)
	if err != nil {
		return fmt.Errorf("opening store: %w", err)
	}
	defer db.Close()
	logger.Info("store ready", slog.String("dsn", cfg.Store.DSN))

	auditLog, err := audit.New(db.DB())
	if err != nil {
		return fmt.Errorf("initializing audit logger: %w", err)
	}
	logger.Info("audit logger ready")

	mem := memory.New(db.DB(), cfg.Memory.ImmutableKeys)
	logger.Info("memory system ready",
		slog.Int("immutable_keys", len(cfg.Memory.ImmutableKeys)),
	)

	masterKeyEnv := cfg.Credentials.MasterKeyEnv
	if masterKeyEnv == "" {
		masterKeyEnv = "PINCER_MASTER_KEY"
	}
	masterKey := os.Getenv(masterKeyEnv)
	var credStore *credentials.Store
	if masterKey != "" {
		credStore, err = credentials.New(db.DB(), masterKey)
		if err != nil {
			return fmt.Errorf("initializing credential store: %w", err)
		}
		logger.Info("credential store ready")
	} else {
		logger.Warn("credential store disabled (set PINCER_MASTER_KEY to enable)")
	}

	soulPath := cfg.Soul.Path
	if soulPath == "" {
		soulPath = filepath.Join(config.DataDir(), "soul.toml")
	}
	soulDef, err := soul.Load(soulPath)
	if err != nil {
		return fmt.Errorf("loading soul: %w", err)
	}
	if err := soulDef.SeedMemory(ctx, mem, "default"); err != nil {
		logger.Warn("soul memory seeding had errors", slog.String("err", err.Error()))
	}
	logger.Info("soul loaded",
		slog.String("name", soulDef.Identity.Name),
		slog.String("role", soulDef.Identity.Role),
	)

	provider, err := createProvider(cfg, logger)
	if err != nil {
		return fmt.Errorf("creating LLM provider: %w", err)
	}
	logger.Info("llm provider ready", slog.String("provider", provider.Name()))

	sb, err := createSandbox(cfg)
	if err != nil {
		return fmt.Errorf("creating sandbox: %w", err)
	}
	logger.Info("tool sandbox ready", slog.String("mode", cfg.Sandbox.Mode))

	registry := tools.DefaultRegistry()
	registry.Register(&tools.MemoryTool{Memory: mem})
	registry.Register(&tools.SoulTool{Soul: soulDef})
	if credStore != nil {
		registry.Register(&tools.CredentialTool{Credentials: credStore})
	}

	skillDir := cfg.Skills.Dir
	if skillDir == "" {
		skillDir = filepath.Join(config.DataDir(), "skills")
	}
	engine := skills.NewEngine(skills.EngineConfig{
		SkillDir:      skillDir,
		AllowUnsigned: cfg.Skills.AllowUnsigned,
	})
	results, err := engine.LoadAll()
	if err != nil {
		logger.Warn("skill loading had errors", slog.String("err", err.Error()))
	}
	for _, r := range results {
		logger.Info("skill loaded",
			slog.String("skill", r.SkillName),
			slog.Bool("safe", r.Safe),
			slog.Int("findings", len(r.Findings)),
		)
		_ = auditLog.Log(ctx, audit.EventSkillLoad, "", "", "system",
			fmt.Sprintf("skill=%s safe=%v findings=%d", r.SkillName, r.Safe, len(r.Findings)))
	}

	systemPrompt := soulDef.Render()
	if cfg.Agent.SystemPrompt != "" {
		systemPrompt += "\n" + cfg.Agent.SystemPrompt
	}
	for _, sk := range engine.List() {
		if sk.Prompt != "" {
			systemPrompt += "\n\n" + sk.Prompt
		}
	}

	logger.Info("skill engine ready",
		slog.Int("skills", len(engine.List())),
		slog.Int("tools", len(registry.Definitions())),
	)

	approvalMode := agent.ApprovalMode(cfg.Agent.ToolApproval)
	approver := agent.NewApprover(approvalMode, nil)

	runtime := agent.NewRuntime(agent.RuntimeConfig{
		Provider:      provider,
		Store:         db,
		Registry:      registry,
		Sandbox:       sb,
		Approver:      approver,
		Model:         cfg.Agent.Model,
		MaxTokens:     cfg.Agent.MaxContextTokens,
		SystemPrompt:  systemPrompt,
		Memory:        mem,
		Audit:         auditLog,
		DefaultPolicy: buildDefaultPolicy(cfg),
	})

	_ = auditLog.Log(ctx, audit.EventConfigChg, "", "", "system",
		fmt.Sprintf("pincer started version=%s provider=%s sandbox=%s", version, provider.Name(), cfg.Sandbox.Mode))

	webhookSecret := os.Getenv("PINCER_WEBHOOK_SECRET")
	webhooks := scheduler.NewWebhookHandler(webhookSecret)

	sched := scheduler.New()
	go sched.Start(ctx)

	chat := webchat.New()
	if err := chat.Start(ctx); err != nil {
		return fmt.Errorf("starting webchat adapter: %w", err)
	}

	var channelAdapters []channels.Adapter
	channelAdapters = append(channelAdapters, initChannelAdapters(ctx, cfg, logger)...)

	if len(channelAdapters) > 0 {
		router := gateway.NewChannelRouter(runtime, channelAdapters, approver, logger, db, auditLog)
		router.Start(ctx)

		registry.Register(&tools.NotifyTool{
			RunAndDeliver: router.RunAndDeliver,
			Send:          router.SendToSession,
			AuditLog:      router.AuditLog,
		})
	}

	gw := gateway.New(gateway.Config{
		Bind:      cfg.Gateway.Bind,
		Port:      cfg.Gateway.Port,
		Runtime:   runtime,
		Chat:      chat,
		Approver:  approver,
		Logger:    logger,
		Webhooks:  webhooks,
		AuthToken: cfg.Gateway.AuthToken,
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

func createProvider(cfg *config.Config, logger *slog.Logger) (llm.Provider, error) {
	model := cfg.Agent.Model

	switch {
	case hasPrefix(model, "claude-"):
		return llm.NewAnthropicProvider("")
	case hasPrefix(model, "gpt-") || hasPrefix(model, "o3-") || hasPrefix(model, "o4-"):
		return llm.NewOpenAIProvider("", "")
	case hasPrefix(model, "gemini-"):
		return llm.NewGeminiProvider("")
	case hasPrefix(model, "ollama/"):
		return llm.NewOllamaProvider("", model[len("ollama/"):])
	default:

		logger.Info("defaulting to anthropic provider", slog.String("model", model))
		return llm.NewAnthropicProvider("")
	}
}

func createSandbox(cfg *config.Config) (sandbox.Sandbox, error) {
	switch cfg.Sandbox.Mode {
	case "container":
		return sandbox.NewContainerSandbox(sandbox.ContainerConfig{
			WorkDir: config.DataDir(),
		})
	default:
		return sandbox.NewProcessSandbox(config.DataDir()), nil
	}
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
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

	if channelEnabled(cfg, "whatsapp") || os.Getenv("WHATSAPP_DB_PATH") != "" {
		wa, err := whatsapp.New("")
		if err != nil {
			logger.Warn("whatsapp adapter skipped", slog.String("err", err.Error()))
		} else if err := wa.Start(ctx); err != nil {
			logger.Error("whatsapp adapter failed to start", slog.String("err", err.Error()))
		} else {
			logger.Info("whatsapp adapter enabled")
			adapters = append(adapters, wa)
		}
	}

	if channelEnabled(cfg, "matrix") || os.Getenv("MATRIX_HOMESERVER") != "" {
		mx, err := matrix.New(matrix.Config{})
		if err != nil {
			logger.Warn("matrix adapter skipped", slog.String("err", err.Error()))
		} else if err := mx.Start(ctx); err != nil {
			logger.Error("matrix adapter failed to start", slog.String("err", err.Error()))
		} else {
			logger.Info("matrix adapter enabled")
			adapters = append(adapters, mx)
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

func buildDefaultPolicy(cfg *config.Config) sandbox.Policy {
	p := sandbox.DefaultPolicy()

	if cfg.Sandbox.MaxTimeout != "" {
		if d, err := time.ParseDuration(cfg.Sandbox.MaxTimeout); err == nil {
			p.Timeout = d
		}
	}

	switch cfg.Sandbox.NetworkPolicy {
	case "allow":
		p.NetworkAccess = sandbox.NetworkAllow
	case "allowlist":
		p.NetworkAccess = sandbox.NetworkAllowList
	case "deny":
		p.NetworkAccess = sandbox.NetworkDeny
	}

	return p
}

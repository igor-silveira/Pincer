package pincer

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
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
	"github.com/igorsilveira/pincer/pkg/a2a"
	"github.com/igorsilveira/pincer/pkg/mcp"
	"github.com/igorsilveira/pincer/pkg/memory"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
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

type storeDeps struct {
	db        *store.Store
	auditLog  *audit.Logger
	mem       *memory.Store
	credStore *credentials.Store
}

func initStorage(cfg *config.Config, logger *slog.Logger) (*storeDeps, error) {
	db, err := store.New(cfg.Store.DSN)
	if err != nil {
		return nil, fmt.Errorf("opening store: %w", err)
	}
	logger.Info("store ready", slog.String("dsn", cfg.Store.DSN))

	auditLog, err := audit.New(db.DB())
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("initializing audit logger: %w", err)
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
			db.Close()
			return nil, fmt.Errorf("initializing credential store: %w", err)
		}
		logger.Info("credential store ready")
	} else {
		logger.Warn("credential store disabled (set PINCER_MASTER_KEY to enable)")
	}

	return &storeDeps{db: db, auditLog: auditLog, mem: mem, credStore: credStore}, nil
}

func initAgent(ctx context.Context, cfg *config.Config, logger *slog.Logger, deps *storeDeps) (*agent.Runtime, *tools.Registry, *agent.Approver, *soul.Soul, error) {
	soulPath := cfg.Soul.Path
	if soulPath == "" {
		soulPath = filepath.Join(config.DataDir(), "soul.toml")
	}
	soulDef, err := soul.Load(soulPath)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("loading soul: %w", err)
	}
	if err := soulDef.SeedMemory(ctx, deps.mem, "default"); err != nil {
		logger.Warn("soul memory seeding had errors", slog.String("err", err.Error()))
	}
	logger.Info("soul loaded",
		slog.String("name", soulDef.Identity.Name),
		slog.String("role", soulDef.Identity.Role),
	)

	provider, err := createProvider(cfg, logger)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("creating LLM provider: %w", err)
	}
	logger.Info("llm provider ready", slog.String("provider", provider.Name()))

	sb, err := createSandbox(cfg)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("creating sandbox: %w", err)
	}
	logger.Info("tool sandbox ready", slog.String("mode", cfg.Sandbox.Mode))

	registry := tools.DefaultRegistry()
	registry.Register(&tools.MemoryTool{Memory: deps.mem})
	registry.Register(&tools.SoulTool{Soul: soulDef})
	if deps.credStore != nil {
		registry.Register(&tools.CredentialTool{Credentials: deps.credStore})
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
		_ = deps.auditLog.Log(ctx, audit.EventSkillLoad, "", "", "system",
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
		Store:         deps.db,
		Registry:      registry,
		Sandbox:       sb,
		Approver:      approver,
		Model:         cfg.Agent.Model,
		MaxTokens:     cfg.Agent.MaxContextTokens,
		SystemPrompt:  systemPrompt,
		Memory:        deps.mem,
		Audit:         deps.auditLog,
		DefaultPolicy: buildDefaultPolicy(cfg),
	})

	_ = deps.auditLog.Log(ctx, audit.EventConfigChg, "", "", "system",
		fmt.Sprintf("pincer started version=%s provider=%s sandbox=%s", version, provider.Name(), cfg.Sandbox.Mode))

	return runtime, registry, approver, soulDef, nil
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
	defer func() { _ = shutdownTracer(context.Background()) }()
	if cfg.Tracing.Enabled {
		logger.Info("opentelemetry tracing enabled", slog.String("endpoint", cfg.Tracing.Endpoint))
	}

	deps, err := initStorage(cfg, logger)
	if err != nil {
		return err
	}
	defer deps.db.Close()

	runtime, registry, approver, soulDef, err := initAgent(ctx, cfg, logger, deps)
	if err != nil {
		return err
	}

	mcpMgr := initMCPServers(ctx, cfg, logger, registry, deps.auditLog)
	if mcpMgr != nil {
		defer mcpMgr.DisconnectAll()
	}

	webhookSecret := os.Getenv("PINCER_WEBHOOK_SECRET")
	webhooks := scheduler.NewWebhookHandler(webhookSecret)

	sched := scheduler.New()
	go sched.Start(ctx)

	chat := webchat.New()
	if err := chat.Start(ctx); err != nil {
		return fmt.Errorf("starting webchat adapter: %w", err)
	}

	channelAdapters := initChannelAdapters(ctx, cfg, logger)

	if len(channelAdapters) > 0 {
		router := gateway.NewChannelRouter(runtime, channelAdapters, approver, logger, deps.db, deps.auditLog)
		router.Start(ctx)

		registry.Register(&tools.NotifyTool{
			RunAndDeliver: router.RunAndDeliver,
			Send:          router.SendToSession,
			AuditLog:      router.AuditLog,
		})
	}

	var a2aHandler http.Handler
	if cfg.A2A.Enabled {
		a2aHandler = initA2AHandler(cfg, runtime, registry, soulDef, deps.auditLog, logger)
	}

	gw := gateway.New(gateway.Config{
		Bind:       cfg.Gateway.Bind,
		Port:       cfg.Gateway.Port,
		Runtime:    runtime,
		Chat:       chat,
		Approver:   approver,
		Logger:     logger,
		Webhooks:   webhooks,
		A2AHandler: a2aHandler,
		AuthToken:  cfg.Gateway.AuthToken,
	})

	logger.Info("pincer gateway ready",
		slog.String("url", fmt.Sprintf("http://127.0.0.1:%d", cfg.Gateway.Port)),
		slog.String("tool_approval", string(agent.ApprovalMode(cfg.Agent.ToolApproval))),
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
		return llm.NewAnthropicProvider(os.Getenv(cfg.Agent.APIKeyEnv), cfg.Agent.BaseURL)
	case hasPrefix(model, "gpt-") || hasPrefix(model, "o3-") || hasPrefix(model, "o4-"):
		return llm.NewOpenAIProvider("", "")
	case hasPrefix(model, "gemini-"):
		return llm.NewGeminiProvider("")
	case hasPrefix(model, "ollama/"):
		return llm.NewOllamaProvider("", model[len("ollama/"):])
	default:

		logger.Info("defaulting to anthropic provider", slog.String("model", model))
		return llm.NewAnthropicProvider(os.Getenv(cfg.Agent.APIKeyEnv), cfg.Agent.BaseURL)
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

type adapterEntry struct {
	name   string
	envVar string
	create func(cfg *config.Config) (channels.Adapter, error)
}

func initChannelAdapters(ctx context.Context, cfg *config.Config, logger *slog.Logger) []channels.Adapter {
	entries := []adapterEntry{
		{"telegram", "TELEGRAM_BOT_TOKEN", func(c *config.Config) (channels.Adapter, error) {
			return telegram.New(channelToken(c, "telegram"))
		}},
		{"discord", "DISCORD_BOT_TOKEN", func(c *config.Config) (channels.Adapter, error) {
			return discord.New(channelToken(c, "discord"))
		}},
		{"slack", "SLACK_BOT_TOKEN", func(c *config.Config) (channels.Adapter, error) {
			return slackadapter.New(channelToken(c, "slack"), os.Getenv("SLACK_APP_TOKEN"))
		}},
		{"whatsapp", "WHATSAPP_DB_PATH", func(c *config.Config) (channels.Adapter, error) {
			return whatsapp.New("")
		}},
		{"matrix", "MATRIX_HOMESERVER", func(c *config.Config) (channels.Adapter, error) {
			return matrix.New(matrix.Config{})
		}},
	}

	var adapters []channels.Adapter
	for _, e := range entries {
		if !channelEnabled(cfg, e.name) && os.Getenv(e.envVar) == "" {
			continue
		}
		a, err := e.create(cfg)
		if err != nil {
			logger.Warn(e.name+" adapter skipped", slog.String("err", err.Error()))
			continue
		}
		if err := a.Start(ctx); err != nil {
			logger.Error(e.name+" adapter failed to start", slog.String("err", err.Error()))
			continue
		}
		logger.Info(e.name + " adapter enabled")
		adapters = append(adapters, a)
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

func initA2AHandler(cfg *config.Config, runtime *agent.Runtime, registry *tools.Registry, soulDef *soul.Soul, auditLog *audit.Logger, logger *slog.Logger) http.Handler {
	card := buildAgentCard(cfg, registry, soulDef)
	handler := a2a.NewHandler(a2a.HandlerConfig{
		Card:      card,
		Runtime:   runtime,
		AuditLog:  auditLog,
		Logger:    logger,
		AuthToken: cfg.A2A.AuthToken,
	})
	logger.Info("a2a server enabled",
		slog.String("name", card.Name),
		slog.Int("skills", len(card.Skills)),
	)
	return handler
}

func buildAgentCard(cfg *config.Config, registry *tools.Registry, soulDef *soul.Soul) *a2a.AgentCard {
	url := cfg.A2A.ExternalURL
	if url == "" {
		url = fmt.Sprintf("http://127.0.0.1:%d", cfg.Gateway.Port)
	}

	var skills []a2a.Skill
	for _, def := range registry.Definitions() {
		skills = append(skills, a2a.Skill{
			ID:          def.Name,
			Name:        def.Name,
			Description: def.Description,
		})
	}

	return &a2a.AgentCard{
		Name:        soulDef.Identity.Name,
		Description: soulDef.Identity.Role,
		URL:         url,
		Version:     version,
		Capabilities: a2a.Capabilities{
			Streaming: true,
		},
		Skills: skills,
	}
}

func initMCPServers(ctx context.Context, cfg *config.Config, logger *slog.Logger, registry *tools.Registry, auditLog *audit.Logger) *mcp.Manager {
	if !cfg.MCP.Enabled || len(cfg.MCP.Servers) == 0 {
		return nil
	}

	mgr := mcp.NewManager(logger)

	for _, srv := range cfg.MCP.Servers {
		if srv.Enabled != nil && !*srv.Enabled {
			continue
		}
		if srv.Command == "" {
			logger.Warn("mcp server has no command, skipping", slog.String("name", srv.Name))
			continue
		}

		mcpTools, err := mgr.Connect(ctx, mcp.ServerConfig{
			Name:    srv.Name,
			Command: srv.Command,
			Args:    srv.Args,
			Env:     srv.Env,
		})
		if err != nil {
			logger.Error("mcp server connect failed",
				slog.String("name", srv.Name),
				slog.String("err", err.Error()),
			)
			continue
		}

		for _, t := range mcpTools {
			name := srv.Name
			sessionFn := func() (*mcpsdk.ClientSession, error) { return mgr.Session(name) }
			registry.Register(mcp.NewMCPTool(name, t, sessionFn))
		}

		_ = auditLog.Log(ctx, audit.EventMCPConnect, "", "", "system",
			fmt.Sprintf("server=%s tools=%d", srv.Name, len(mcpTools)))

		logger.Info("mcp server ready",
			slog.String("name", srv.Name),
			slog.Int("tools", len(mcpTools)),
		)
	}

	return mgr
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

	p.AllowedPaths = cfg.Sandbox.AllowedPaths
	p.ReadOnlyPaths = cfg.Sandbox.ReadOnlyPaths

	return p
}

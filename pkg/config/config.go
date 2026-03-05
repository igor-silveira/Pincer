package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Gateway     GatewayConfig            `toml:"gateway"`
	Agent       AgentConfig              `toml:"agent"`
	Channels    map[string]ChannelConfig `toml:"channels"`
	Sandbox     SandboxConfig            `toml:"sandbox"`
	Memory      MemoryConfig             `toml:"memory"`
	Store       StoreConfig              `toml:"store"`
	Log         LogConfig                `toml:"log"`
	Tracing     TracingConfig            `toml:"tracing"`
	Skills      SkillsConfig             `toml:"skills"`
	Credentials CredentialsConfig        `toml:"credentials"`
	Soul        SoulConfig               `toml:"soul"`
	MCP         MCPConfig                `toml:"mcp"`
	A2A         A2AConfig                `toml:"a2a"`
	Browser     BrowserConfig            `toml:"browser"`
}

type GatewayConfig struct {
	Bind      string `toml:"bind"`
	Port      int    `toml:"port"`
	AuthToken string `toml:"auth_token"`
}

type AgentConfig struct {
	Model             string   `toml:"model"`
	APIKeyEnv         string   `toml:"api_key_env"`
	BaseURL           string   `toml:"base_url"`
	FallbackModels    []string `toml:"fallback_models"`
	MaxContextTokens  int      `toml:"max_context_tokens"`
	MaxToolIterations int      `toml:"max_tool_iterations"`
	ToolApproval      string   `toml:"tool_approval"`
	SystemPrompt      string   `toml:"system_prompt"`
}

type ChannelConfig struct {
	Enabled   bool     `toml:"enabled"`
	Token     string   `toml:"token"`
	TokenEnv  string   `toml:"token_env"`
	AllowList []string `toml:"allow_list"`
}

type SandboxConfig struct {
	Mode          string   `toml:"mode"`
	NetworkPolicy string   `toml:"network_policy"`
	MaxTimeout    string   `toml:"max_timeout"`
	AllowedPaths  []string `toml:"allowed_paths"`
	ReadOnlyPaths []string `toml:"read_only_paths"`
}

type MemoryConfig struct {
	ImmutableKeys []string `toml:"immutable_keys"`
	MaxVersions   int      `toml:"max_versions"`
	VectorSearch  bool     `toml:"vector_search"`
}

type StoreConfig struct {
	Driver string `toml:"driver"`
	DSN    string `toml:"dsn"`
}

type LogConfig struct {
	Level  string `toml:"level"`
	Format string `toml:"format"`
}

type TracingConfig struct {
	Enabled  bool   `toml:"enabled"`
	Endpoint string `toml:"endpoint"`
}

type SkillsConfig struct {
	Dir           string `toml:"dir"`
	AllowUnsigned bool   `toml:"allow_unsigned"`
}

type CredentialsConfig struct {
	MasterKeyEnv string `toml:"master_key_env"`
}

type SoulConfig struct {
	Path string `toml:"path"`
}

type MCPConfig struct {
	Enabled bool              `toml:"enabled"`
	Servers []MCPServerConfig `toml:"servers"`
}

type MCPServerConfig struct {
	Name    string            `toml:"name"`
	Command string            `toml:"command"`
	Args    []string          `toml:"args"`
	Env     map[string]string `toml:"env"`
	URL     string            `toml:"url"`
	Enabled *bool             `toml:"enabled"`
}

type A2AConfig struct {
	Enabled     bool   `toml:"enabled"`
	AuthToken   string `toml:"auth_token"`
	ExternalURL string `toml:"external_url"`
}

type BrowserConfig struct {
	Enabled     bool   `toml:"enabled"`
	Headless    bool   `toml:"headless"`
	IdleTimeout string `toml:"idle_timeout"`
}

func Default() *Config {
	return &Config{
		Gateway: GatewayConfig{
			Bind: "loopback",
			Port: 18789,
		},
		Agent: AgentConfig{
			Model:             "claude-sonnet-4-20250514",
			MaxContextTokens:  128000,
			MaxToolIterations: 25,
			ToolApproval:      "ask",
		},
		Sandbox: SandboxConfig{
			Mode:          "process",
			NetworkPolicy: "deny",
			MaxTimeout:    "5m",
		},
		Memory: MemoryConfig{
			ImmutableKeys: []string{"identity", "core_values"},
			MaxVersions:   100,
		},
		Store: StoreConfig{
			Driver: "sqlite",
			DSN:    filepath.Join(DataDir(), "pincer.db"),
		},
		Log: LogConfig{
			Level:  "info",
			Format: "json",
		},
		Browser: BrowserConfig{
			Enabled:     false,
			Headless:    true,
			IdleTimeout: "10m",
		},
	}
}

var (
	current *Config
	mu      sync.RWMutex
)

func Load(path string) (*Config, error) {
	cfg := Default()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	if err := toml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config file: %w", err)
	}

	if cfg.Store.DSN == "" {
		cfg.Store.DSN = filepath.Join(DataDir(), "pincer.db")
	}

	mu.Lock()
	current = cfg
	mu.Unlock()

	return cfg, nil
}

func Current() *Config {
	mu.RLock()
	defer mu.RUnlock()
	if current == nil {
		return Default()
	}
	cp := *current
	return &cp
}

func DataDir() string {
	if dir := os.Getenv("PINCER_DATA_DIR"); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".pincer"
	}
	return filepath.Join(home, ".pincer")
}

func DefaultConfigPath() string {
	return filepath.Join(DataDir(), "pincer.toml")
}

func EnsureDataDir() error {
	return os.MkdirAll(DataDir(), 0700)
}

func Save(cfg *Config, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("creating config file: %w", err)
	}
	defer f.Close()

	if err := toml.NewEncoder(f).Encode(cfg); err != nil {
		return fmt.Errorf("encoding config: %w", err)
	}
	return nil
}

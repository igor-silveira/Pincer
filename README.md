# Pincer

A self-hosted, security-first AI assistant gateway written in Go. Pincer connects to messaging platforms, executes real-world tasks through sandboxed tools, and manages long-running conversations with persistent memory - all from a single binary.

```
┌──────────────────────────────────────────────────┐
│                 Pincer Gateway                   │
│   ┌────────┐  ┌───────────┐  ┌────────────────┐  │
│   │  HTTP  │  │ WebSocket │  │     gRPC       │  │
│   └───┬────┘  └─────┬─────┘  └──────┬─────────┘  │
│       └──────────┬──┘───────────────┘            │
│             ┌────▼─────┐                         │
│             │  Router  │                         │
│             └────┬─────┘                         │
│       ┌──────────┼──────────┐                    │
│  ┌────▼────┐ ┌───▼─────┐ ┌──▼───────┐            │
│  │ Channel │ │ Session │ │  Agent   │            │
│  │ Manager │ │  Store  │ │ Runtime  │            │
│  └─────────┘ └─────────┘ └──────────┘            │
└──────────────────────────────────────────────────┘
```

## Features

- **Multi-channel messaging** - Telegram, Discord, Slack, WhatsApp, Matrix, and WebChat from a single instance
- **Agentic loop** - Multi-turn conversations with streaming, tool calling, and context management
- **Sandboxed tool execution** - Process-level and container-level isolation for shell, file, HTTP, and browser tools
- **Multiple LLM providers** - Anthropic, OpenAI, Gemini, and Ollama with automatic model routing
- **Encrypted credentials** - AES-256-GCM encryption with Argon2id key derivation
- **Persistent memory** - Structured key-value memory with immutable key protection and content-addressed hashing
- **Skill engine** - Loadable skills with Ed25519 signature verification and static analysis
- **Human-in-the-loop** - Configurable tool approval modes (auto, ask, deny)
- **Smart context windowing** - Hash-based change detection to avoid redundant token usage
- **Observability** - Structured logging via slog, Prometheus metrics, OpenTelemetry tracing, and append-only audit log
- **Companion devices** - gRPC-based node system for mobile and desktop peripherals
- **Proactive messaging** - Schedule delayed turns and send messages via the notify tool, with full audit logging
- **MCP client** - Connect to external MCP servers (via stdio) and import their tools as native Pincer tools
- **A2A server** - Expose Pincer as an Agent-to-Agent protocol endpoint with Agent Card discovery, task management, and SSE streaming
- **Scheduler** - Interval-based cron jobs and HMAC-signed webhook ingestion
- **Single binary** - Zero runtime dependencies, pure Go (no CGo)

## Quick Start

### Build from source

```bash
git clone https://github.com/igorsilveira/pincer.git
cd pincer
go build -o pincer .
```

### Run

```bash
# Set your LLM provider API key
export ANTHROPIC_API_KEY="sk-..."

# Start the gateway
./pincer start
```

Pincer listens on `http://127.0.0.1:18789` by default. Open it in your browser to access the WebChat interface.

### Configuration

Pincer uses a TOML configuration file at `~/.pincer/pincer.toml`. A sensible default configuration is used when no file exists.

```toml
[gateway]
bind = "loopback"
port = 18789

[agent]
model = "claude-sonnet-4-20250514"
max_context_tokens = 128000
tool_approval = "ask"  # auto | ask | deny
system_prompt = ""

[store]
driver = "sqlite"
dsn = ""  # defaults to ~/.pincer/pincer.db

[sandbox]
mode = "process"       # process | container
network_policy = "deny"
max_timeout = "5m"
allowed_paths = []     # restrict tool filesystem access (empty = allow all)
read_only_paths = []   # prevent writes to these directories

[memory]
immutable_keys = ["identity", "core_values"]
max_versions = 100

[credentials]
master_key_env = "PINCER_MASTER_KEY"

[skills]
dir = ""               # defaults to ~/.pincer/skills
allow_unsigned = false

[log]
level = "info"
format = "json"

[tracing]
enabled = false
endpoint = "localhost:4318"

[mcp]
enabled = true

[[mcp.servers]]
name = "github"
command = "npx"
args = ["-y", "@modelcontextprotocol/server-github"]
env = { GITHUB_TOKEN = "ghp_..." }

[a2a]
enabled = true
auth_token = "secret-a2a-token"
external_url = "https://pincer.example.com"
```

### Channel adapters

Enable messaging channels by setting their tokens either in the config file or via environment variables:

```toml
[channels.telegram]
enabled = true
token_env = "TELEGRAM_BOT_TOKEN"

[channels.discord]
enabled = true
token_env = "DISCORD_BOT_TOKEN"

[channels.slack]
enabled = true
token_env = "SLACK_BOT_TOKEN"

[channels.whatsapp]
enabled = true

[channels.matrix]
enabled = true
```

| Channel   | Environment Variables                                   |
|-----------|---------------------------------------------------------|
| Telegram  | `TELEGRAM_BOT_TOKEN`                                    |
| Discord   | `DISCORD_BOT_TOKEN`                                     |
| Slack     | `SLACK_BOT_TOKEN`, `SLACK_APP_TOKEN`                    |
| WhatsApp  | `WHATSAPP_DB_PATH` (uses whatsmeow multi-device)       |
| Matrix    | `MATRIX_HOMESERVER`, `MATRIX_USER_ID`, `MATRIX_TOKEN`  |
| WebChat   | Built-in, always available at the gateway URL           |

## CLI Commands

```
pincer start       Start the gateway
pincer status      Show gateway status
pincer chat        Open the TUI chat interface
pincer doctor      Run system diagnostics
pincer backup      Create a backup of the data directory
pincer restore     Restore from a backup file
pincer audit       View the audit log
pincer version     Print version
```

## Architecture

### Project structure

```
cmd/pincer/           CLI commands (cobra)
pkg/
  agent/              Agentic loop, approval flow, context windowing, compaction
    tools/            Tool registry (shell, file, http, browser, memory, credential, notify)
  audit/              Append-only audit log
  channels/           Channel adapter interface and implementations
    discord/            Discord adapter (discordgo)
    matrix/             Matrix adapter (mautrix)
    slack/              Slack adapter (slack-go)
    telegram/           Telegram adapter (go-telegram/bot)
    webchat/            WebChat adapter (built-in)
    whatsapp/           WhatsApp adapter (whatsmeow)
  a2a/                A2A protocol server (Agent Card, JSON-RPC, task store)
  config/             TOML configuration loading
  credentials/        AES-256-GCM encrypted credential store
  gateway/            HTTP/WebSocket server and routing
  llm/                LLM provider interface and implementations
    anthropic.go        Anthropic Messages API
    openai.go           OpenAI Chat Completions API
    gemini.go           Google Gemini API
    ollama.go           Ollama (OpenAI-compatible)
  mcp/                MCP client (server connections, tool import)
  memory/             Persistent structured memory
  nodes/              gRPC companion device hub
  sandbox/            Process and container sandboxing
  scheduler/          Cron jobs and webhook handler
  skills/             Skill loading, signing, and static analysis
  store/              SQLite store (sessions, messages, memory, credentials)
  telemetry/          Logging, metrics, and tracing
  tui/                Terminal chat UI (bubbletea)
```

### LLM providers

Pincer routes to the correct provider based on the model name prefix:

| Prefix        | Provider  |
|---------------|-----------|
| `claude-`     | Anthropic |
| `gpt-`, `o3-`, `o4-` | OpenAI |
| `gemini-`     | Gemini    |
| `ollama/`     | Ollama    |

### Sandbox tiers

**Process isolation** (default) - Runs tools as child processes with timeout enforcement, output limits, work directory validation, and environment restrictions. File tools enforce `allowed_paths` and `read_only_paths` with symlink-aware path resolution. HTTP and browser tools respect the `network_policy` setting.

**Container isolation** - Runs tools inside ephemeral containers with read-only root filesystem, dropped capabilities, no network by default, and memory/PID limits.

### MCP client

Pincer can connect to external [Model Context Protocol](https://modelcontextprotocol.io/) servers and import their tools. Each MCP server runs as a subprocess (stdio transport), and its tools are registered with a `mcp_{server}__{tool}` naming convention.

```toml
[mcp]
enabled = true

[[mcp.servers]]
name = "github"
command = "npx"
args = ["-y", "@modelcontextprotocol/server-github"]
env = { GITHUB_TOKEN = "ghp_..." }

[[mcp.servers]]
name = "filesystem"
command = "npx"
args = ["-y", "@modelcontextprotocol/server-filesystem", "/home/user/data"]
```

MCP connections and disconnections are recorded in the audit log.

### A2A server

Pincer can expose itself as an [Agent-to-Agent (A2A)](https://google.github.io/A2A/) compatible agent. Other agents can discover Pincer via its Agent Card and send tasks over JSON-RPC 2.0.

| Endpoint | Description |
|---|---|
| `GET /.well-known/agentcard` | Agent Card discovery |
| `POST /a2a` | JSON-RPC 2.0 dispatch |
| `POST /a2a/messages` | Send message (synchronous) |
| `POST /a2a/messages:stream` | Send message (SSE streaming) |
| `GET /a2a/tasks/{id}` | Get task status |
| `GET /a2a/tasks` | List tasks |
| `POST /a2a/tasks/{id}:cancel` | Cancel a task |

```toml
[a2a]
enabled = true
auth_token = "secret-a2a-token"
external_url = "https://pincer.example.com"
```

### Security model

- All tool executions go through the sandbox with configurable network and filesystem policies; path checks resolve symlinks to prevent escape attacks
- Credential storage uses AES-256-GCM with keys derived via Argon2id from a master passphrase
- Skills require Ed25519 signatures by default; unsigned skills need explicit opt-in
- Static analysis scans skills for exfiltration patterns on load
- Immutable memory keys prevent the agent from modifying its own core identity
- Append-only audit log records all tool executions, memory changes, and configuration changes
- WebSocket and HTTP endpoints bind to loopback by default

## Docker

```bash
# Build the image
docker build -t pincer:latest .

# Run with a persistent data volume
docker run -d -p 18789:18789 -v pincer-data:/data \
  -e ANTHROPIC_API_KEY="sk-..." \
  pincer:latest
```

Pass `--build-arg VERSION=x.y.z` to embed a specific version at build time.

## Development

### Prerequisites

- Go 1.26+
- Chrome/Chromium (optional, for browser tool)
- Docker (optional, for container sandbox)

### Build and test

```bash
go build ./...
go test ./...
go vet ./...
```

### Running with Ollama (local models)

```bash
# Start Ollama
ollama serve

# Pull a model
ollama pull llama3

# Run Pincer with Ollama
./pincer start --config pincer.toml.example
```

With the config:

```toml
[agent]
model = "ollama/llama3"
```

## Environment Variables

| Variable                | Description                              |
|-------------------------|------------------------------------------|
| `ANTHROPIC_API_KEY`     | Anthropic API key                        |
| `OPENAI_API_KEY`        | OpenAI API key                           |
| `GEMINI_API_KEY`        | Google Gemini API key                    |
| `OLLAMA_BASE_URL`       | Ollama server URL (default: localhost)   |
| `PINCER_MASTER_KEY`     | Master key for credential encryption     |
| `PINCER_DATA_DIR`       | Data directory (default: ~/.pincer)      |
| `PINCER_WEBHOOK_SECRET` | HMAC secret for webhook verification     |

## License

MIT

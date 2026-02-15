package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"sync"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type ServerConfig struct {
	Name    string
	Command string
	Args    []string
	Env     map[string]string
}

type serverConn struct {
	session *mcpsdk.ClientSession
	tools   []*mcpsdk.Tool
}

type Manager struct {
	mu      sync.Mutex
	client  *mcpsdk.Client
	servers map[string]*serverConn
	logger  *slog.Logger
}

func NewManager(logger *slog.Logger) *Manager {
	if logger == nil {
		logger = slog.Default()
	}
	client := mcpsdk.NewClient(&mcpsdk.Implementation{
		Name:    "pincer",
		Version: "1.0.0",
	}, nil)
	return &Manager{
		client:  client,
		servers: make(map[string]*serverConn),
		logger:  logger,
	}
}

func (m *Manager) Connect(ctx context.Context, cfg ServerConfig) ([]*mcpsdk.Tool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.servers[cfg.Name]; ok {
		return nil, fmt.Errorf("mcp: server %q already connected", cfg.Name)
	}

	cmd := exec.CommandContext(ctx, cfg.Command, cfg.Args...)
	for k, v := range cfg.Env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	transport := &mcpsdk.CommandTransport{Command: cmd}
	session, err := m.client.Connect(ctx, transport, nil)
	if err != nil {
		return nil, fmt.Errorf("mcp: connecting to %q: %w", cfg.Name, err)
	}

	var tools []*mcpsdk.Tool
	result, err := session.ListTools(ctx, nil)
	if err != nil {
		_ = session.Close()
		return nil, fmt.Errorf("mcp: listing tools from %q: %w", cfg.Name, err)
	}
	tools = result.Tools

	m.servers[cfg.Name] = &serverConn{
		session: session,
		tools:   tools,
	}

	m.logger.Info("mcp server connected",
		slog.String("server", cfg.Name),
		slog.Int("tools", len(tools)),
	)

	return tools, nil
}

func (m *Manager) Session(name string) (*mcpsdk.ClientSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	conn, ok := m.servers[name]
	if !ok {
		return nil, fmt.Errorf("mcp: server %q not connected", name)
	}
	return conn.session, nil
}

func (m *Manager) Disconnect(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	conn, ok := m.servers[name]
	if !ok {
		return fmt.Errorf("mcp: server %q not connected", name)
	}
	delete(m.servers, name)
	m.logger.Info("mcp server disconnected", slog.String("server", name))
	return conn.session.Close()
}

func (m *Manager) DisconnectAll() {
	m.mu.Lock()
	names := make([]string, 0, len(m.servers))
	for name := range m.servers {
		names = append(names, name)
	}
	m.mu.Unlock()

	for _, name := range names {
		if err := m.Disconnect(name); err != nil {
			m.logger.Warn("mcp: error disconnecting server",
				slog.String("server", name),
				slog.String("err", err.Error()),
			)
		}
	}
}

func (m *Manager) AllTools() []ToolInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	var all []ToolInfo
	for serverName, conn := range m.servers {
		for _, t := range conn.tools {
			all = append(all, ToolInfo{
				ServerName: serverName,
				Tool:       t,
			})
		}
	}
	return all
}

type ToolInfo struct {
	ServerName string
	Tool       *mcpsdk.Tool
}

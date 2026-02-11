package nodes

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/igorsilveira/pincer/pkg/telemetry"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type Node struct {
	ID           string
	Name         string
	Address      string
	ConnectedAt  time.Time
	LastPing     time.Time
	Capabilities []string
}

type Hub struct {
	mu      sync.RWMutex
	nodes   map[string]*connectedNode
	server  *grpc.Server
	handler MessageHandler
}

type connectedNode struct {
	node   Node
	conn   *grpc.ClientConn
	cancel context.CancelFunc
}

type MessageHandler func(ctx context.Context, nodeID, message string) (string, error)

func NewHub(handler MessageHandler) *Hub {
	return &Hub{
		nodes:   make(map[string]*connectedNode),
		handler: handler,
	}
}

func (h *Hub) StartServer(ctx context.Context, addr string) error {
	logger := telemetry.FromContext(ctx)

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("nodes: listen %s: %w", addr, err)
	}

	h.server = grpc.NewServer()
	registerNodeService(h.server, h)

	logger.Info("node hub listening", slog.String("addr", addr))

	go func() {
		<-ctx.Done()
		h.server.GracefulStop()
	}()

	go func() {
		if err := h.server.Serve(ln); err != nil {
			logger.Error("node hub serve error", slog.String("err", err.Error()))
		}
	}()

	return nil
}

func (h *Hub) Connect(ctx context.Context, id, name, address string) error {
	conn, err := grpc.NewClient(address,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return fmt.Errorf("nodes: connecting to %s: %w", address, err)
	}

	nodeCtx, cancel := context.WithCancel(ctx)

	cn := &connectedNode{
		node: Node{
			ID:           id,
			Name:         name,
			Address:      address,
			ConnectedAt:  time.Now(),
			LastPing:     time.Now(),
			Capabilities: []string{},
		},
		conn:   conn,
		cancel: cancel,
	}

	h.mu.Lock()
	h.nodes[id] = cn
	h.mu.Unlock()

	go h.keepalive(nodeCtx, cn)

	return nil
}

func (h *Hub) Disconnect(id string) error {
	h.mu.Lock()
	cn, ok := h.nodes[id]
	if ok {
		delete(h.nodes, id)
	}
	h.mu.Unlock()

	if !ok {
		return fmt.Errorf("nodes: %q not found", id)
	}

	cn.cancel()
	return cn.conn.Close()
}

func (h *Hub) List() []Node {
	h.mu.RLock()
	defer h.mu.RUnlock()

	out := make([]Node, 0, len(h.nodes))
	for _, cn := range h.nodes {
		out = append(out, cn.node)
	}
	return out
}

func (h *Hub) SendToNode(ctx context.Context, nodeID, message string) (string, error) {
	h.mu.RLock()
	cn, ok := h.nodes[nodeID]
	h.mu.RUnlock()

	if !ok {
		return "", fmt.Errorf("nodes: %q not connected", nodeID)
	}

	_ = cn
	return "", fmt.Errorf("nodes: remote message dispatch not yet implemented for %q", nodeID)
}

func (h *Hub) keepalive(ctx context.Context, cn *connectedNode) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h.mu.Lock()
			cn.node.LastPing = time.Now()
			h.mu.Unlock()
		}
	}
}

func registerNodeService(s *grpc.Server, h *Hub) {

	_ = s
	_ = h
}

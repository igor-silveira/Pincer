package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/igorsilveira/pincer/pkg/llm"
	"github.com/igorsilveira/pincer/pkg/sandbox"
)

type Tool interface {
	Definition() llm.ToolDefinition

	Execute(ctx context.Context, input json.RawMessage, sb sandbox.Sandbox, policy sandbox.Policy) (string, error)
}

type Registry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]Tool)}
}

func (r *Registry) Register(t Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[t.Definition().Name] = t
}

func (r *Registry) Unregister(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.tools, name)
}

func (r *Registry) Get(name string) (Tool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	if !ok {
		return nil, fmt.Errorf("unknown tool: %s", name)
	}
	return t, nil
}

func (r *Registry) Definitions() []llm.ToolDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	defs := make([]llm.ToolDefinition, 0, len(r.tools))
	for _, t := range r.tools {
		defs = append(defs, t.Definition())
	}
	return defs
}

func (r *Registry) Filter(names []string) *Registry {
	if len(names) == 0 {
		return r
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	filtered := NewRegistry()
	for _, name := range names {
		if t, ok := r.tools[name]; ok {
			filtered.tools[name] = t
		}
	}
	return filtered
}

func (r *Registry) Without(names []string) *Registry {
	if len(names) == 0 {
		return r
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	exclude := make(map[string]bool, len(names))
	for _, name := range names {
		exclude[name] = true
	}
	filtered := NewRegistry()
	for name, t := range r.tools {
		if !exclude[name] {
			filtered.tools[name] = t
		}
	}
	return filtered
}

func DefaultRegistry() *Registry {
	r := NewRegistry()
	r.Register(&ShellTool{})
	r.Register(&FileReadTool{})
	r.Register(&FileWriteTool{})
	r.Register(&HTTPTool{})
	return r
}

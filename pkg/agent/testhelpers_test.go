package agent

import (
	"context"
	"encoding/json"

	"github.com/igorsilveira/pincer/pkg/llm"
	"github.com/igorsilveira/pincer/pkg/sandbox"
)

type fakeProvider struct {
	events []llm.ChatEvent
	gotReq *llm.ChatRequest
	err    error
	calls  int
}

func (f *fakeProvider) Name() string            { return "fake" }
func (f *fakeProvider) SupportsStreaming() bool { return true }
func (f *fakeProvider) SupportsToolUse() bool   { return true }
func (f *fakeProvider) Models() []llm.ModelInfo {
	return []llm.ModelInfo{{ID: "fake-1", Name: "Fake", MaxContextTokens: 128000}}
}

func (f *fakeProvider) Chat(_ context.Context, req llm.ChatRequest) (<-chan llm.ChatEvent, error) {
	f.gotReq = &req
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	ch := make(chan llm.ChatEvent, len(f.events)+1)
	for _, e := range f.events {
		ch <- e
	}
	close(ch)
	return ch, nil
}

type fakeProviderMulti struct {
	responses [][]llm.ChatEvent
	calls     int
}

func (f *fakeProviderMulti) Name() string            { return "fake-multi" }
func (f *fakeProviderMulti) SupportsStreaming() bool { return true }
func (f *fakeProviderMulti) SupportsToolUse() bool   { return true }
func (f *fakeProviderMulti) Models() []llm.ModelInfo {
	return []llm.ModelInfo{{ID: "fake-1", Name: "Fake", MaxContextTokens: 128000}}
}

func (f *fakeProviderMulti) Chat(_ context.Context, _ llm.ChatRequest) (<-chan llm.ChatEvent, error) {
	idx := f.calls
	f.calls++
	if idx >= len(f.responses) {
		idx = len(f.responses) - 1
	}
	events := f.responses[idx]
	ch := make(chan llm.ChatEvent, len(events)+1)
	for _, e := range events {
		ch <- e
	}
	close(ch)
	return ch, nil
}

type fakeSandboxAgent struct {
	result *sandbox.Result
	err    error
}

func (f *fakeSandboxAgent) Exec(_ context.Context, _ sandbox.Command, _ sandbox.Policy) (*sandbox.Result, error) {
	return f.result, f.err
}

func toolCallEvents(id, name string, input json.RawMessage) []llm.ChatEvent {
	return []llm.ChatEvent{
		{
			Type:     llm.EventToolCall,
			ToolCall: &llm.ToolCall{ID: id, Name: name, Input: input},
		},
		{Type: llm.EventDone, Usage: &llm.Usage{InputTokens: 10, OutputTokens: 5}},
	}
}

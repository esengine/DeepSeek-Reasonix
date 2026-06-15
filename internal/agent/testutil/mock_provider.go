// Package testutil provides reusable test helpers for the agent package.
// MockProvider replaces a real LLM backend in agent tests with scripted
// responses, request recording, and error injection — so the harness
// loop, cache behaviour, and tool dispatch can be verified without
// network calls.
package testutil

import (
	"context"
	"fmt"
	"sync"

	"reasonix/internal/provider"
)

// Turn describes one expected Stream call: the text, optional reasoning,
// optional tool calls, usage telemetry, and optionally an error to inject.
type Turn struct {
	Text      string
	Reasoning string
	ToolCalls []provider.ToolCall
	Usage     *provider.Usage
	// Chunks, when non-empty, is emitted exactly as provided. It is useful for
	// edge cases such as partial tool-call starts followed by an error.
	Chunks []provider.Chunk

	// StreamError, when set, causes Stream to return this error before any
	// chunks, simulating a network or auth failure for that turn.
	StreamError error
	// ChunkError, when set, is emitted after the scripted chunks, simulating a
	// mid-stream provider failure after partial output has reached the agent.
	ChunkError error
}

// MockProvider is a provider.Provider whose Stream returns scripted
// responses, one Turn per call. It records every request it receives so
// tests can inspect what was sent to the model (cache surface, tool
// schemas, message ordering).
//
// Usage:
//
//	mp := NewMock("test-model", Turn{Text: "Hello"}).Record()
//	agent := agent.New(mp, registry, session, opts, nil)
//	agent.Run(ctx, "hi")
//
//	for i, req := range mp.Requests() {
//	    fmt.Printf("turn %d: %d messages, %d tools\n", i+1,
//	        len(req.Messages), len(req.Tools))
//	}
type MockProvider struct {
	mu     sync.Mutex
	name   string
	script []Turn
	seen   int
	reqs   []provider.Request
}

// NewMock creates a MockProvider. The turns argument is the script; each
// Stream call consumes one Turn. Extra calls after the script are exhausted
// return an error. Use Append or SetScript to add more turns later.
func NewMock(name string, turns ...Turn) *MockProvider {
	return &MockProvider{name: name, script: turns}
}

// Name returns the provider instance name.
func (p *MockProvider) Name() string { return p.name }

// Stream replays the next scripted turn. It records the request, then
// sends chunks in order (reasoning → text → tool calls → usage → done).
// If the Turn has StreamError set it is returned immediately.
func (p *MockProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	p.mu.Lock()
	p.reqs = append(p.reqs, req)
	if p.seen >= len(p.script) {
		p.mu.Unlock()
		return nil, fmt.Errorf("MockProvider[%s]: no scripted turn %d (have %d turns)", p.name, p.seen, len(p.script))
	}
	t := p.script[p.seen]
	p.seen++
	p.mu.Unlock()

	if t.StreamError != nil {
		return nil, t.StreamError
	}

	var chunks []provider.Chunk
	if len(t.Chunks) > 0 {
		chunks = append(chunks, t.Chunks...)
	} else {
		if t.Reasoning != "" {
			chunks = append(chunks, provider.Chunk{Type: provider.ChunkReasoning, Text: t.Reasoning})
		}
		if t.Text != "" {
			chunks = append(chunks, provider.Chunk{Type: provider.ChunkText, Text: t.Text})
		}
		for i := range t.ToolCalls {
			tc := t.ToolCalls[i]
			chunks = append(chunks, provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &tc})
		}
		if t.Usage != nil {
			chunks = append(chunks, provider.Chunk{Type: provider.ChunkUsage, Usage: t.Usage})
		}
		if t.ChunkError != nil {
			chunks = append(chunks, provider.Chunk{Type: provider.ChunkError, Err: t.ChunkError})
		} else {
			chunks = append(chunks, provider.Chunk{Type: provider.ChunkDone})
		}
	}

	ch := make(chan provider.Chunk)
	go func() {
		defer close(ch)
		for _, c := range chunks {
			if err := ctx.Err(); err != nil {
				ch <- provider.Chunk{Type: provider.ChunkError, Err: err}
				return
			}
			select {
			case <-ctx.Done():
				ch <- provider.Chunk{Type: provider.ChunkError, Err: ctx.Err()}
				return
			case ch <- c:
			}
		}
	}()
	return ch, nil
}

// Requests returns all recorded requests in call order. Safe to call from
// any goroutine after the run loop finishes.
func (p *MockProvider) Requests() []provider.Request {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]provider.Request, len(p.reqs))
	copy(out, p.reqs)
	return out
}

// LastRequest returns the most recent request, or nil if none.
func (p *MockProvider) LastRequest() *provider.Request {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.reqs) == 0 {
		return nil
	}
	r := p.reqs[len(p.reqs)-1]
	return &r
}

// MessageCount is a shortcut for len(Requests()).
func (p *MockProvider) CallCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.seen
}

// SetScript replaces the script and resets the call counter.
func (p *MockProvider) SetScript(turns ...Turn) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.script = turns
	p.seen = 0
}

// Append adds turns to the existing script.
func (p *MockProvider) Append(turns ...Turn) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.script = append(p.script, turns...)
}

// Reset clears recorded requests and the call counter without changing the script.
func (p *MockProvider) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.reqs = nil
	p.seen = 0
}

// UsageTurn is a convenience: a Turn whose text is empty but usage is set.
// Useful for simulating a tool-call round-trip where the final model response
// that round is tested later.
func UsageTurn(hit, miss, completion int) Turn {
	return Turn{
		Usage: &provider.Usage{
			CacheHitTokens:   hit,
			CacheMissTokens:  miss,
			CompletionTokens: completion,
			PromptTokens:     hit + miss,
			TotalTokens:      hit + miss + completion,
		},
	}
}

// ErrorTurn is a convenience: a Turn that immediately returns the given error.
func ErrorTurn(err error) Turn {
	return Turn{StreamError: err}
}

var _ provider.Provider = (*MockProvider)(nil)

package agent

import (
	"context"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

type cacheDiagProvider struct {
	chunks [][]provider.Chunk
	calls  int
}

func (p *cacheDiagProvider) Name() string { return "cache-diag" }

func (p *cacheDiagProvider) Stream(_ context.Context, _ provider.Request) (<-chan provider.Chunk, error) {
	chunks := p.chunks[p.calls]
	p.calls++
	ch := make(chan provider.Chunk, len(chunks))
	for _, chunk := range chunks {
		ch <- chunk
	}
	close(ch)
	return ch, nil
}

func TestRunPopulatesCacheDiagnosticsOnUsageEvents(t *testing.T) {
	prov := &cacheDiagProvider{chunks: [][]provider.Chunk{
		{
			{Type: provider.ChunkText, Text: "first"},
			{Type: provider.ChunkUsage, Usage: &provider.Usage{
				PromptTokens: 100, CompletionTokens: 10, TotalTokens: 110,
				CacheHitTokens: 0, CacheMissTokens: 100,
			}},
		},
		{
			{Type: provider.ChunkText, Text: "second"},
			{Type: provider.ChunkUsage, Usage: &provider.Usage{
				PromptTokens: 100, CompletionTokens: 10, TotalTokens: 110,
				CacheHitTokens: 80, CacheMissTokens: 20,
			}},
		},
	}}
	reg := tool.NewRegistry()
	var diagnostics []*event.CacheDiagnostics
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind == event.Usage {
			diagnostics = append(diagnostics, e.CacheDiagnostics)
		}
	})
	session := NewSession("stable system")
	session.IncrementRewrite()
	a := New(prov, reg, session, Options{}, sink)

	if err := a.Run(context.Background(), "one"); err != nil {
		t.Fatalf("first Run: %v", err)
	}
	reg.Add(fakeTool{name: "read_file", readOnly: true})
	if err := a.Run(context.Background(), "two"); err != nil {
		t.Fatalf("second Run: %v", err)
	}

	if len(diagnostics) != 2 {
		t.Fatalf("got %d usage diagnostics, want 2", len(diagnostics))
	}
	first, second := diagnostics[0], diagnostics[1]
	if first == nil || second == nil {
		t.Fatalf("diagnostics should be populated on every usage event: first=%v second=%v", first, second)
	}
	if first.PrefixChanged {
		t.Fatalf("first usage should not report a changed prefix: %+v", first)
	}
	if first.CacheMissTokens != 100 || first.CacheHitTokens != 0 {
		t.Fatalf("first cache tokens = hit %d miss %d, want hit 0 miss 100", first.CacheHitTokens, first.CacheMissTokens)
	}
	if !second.PrefixChanged {
		t.Fatalf("second usage should report the tool prefix change: %+v", second)
	}
	if len(second.PrefixChangeReasons) != 1 || second.PrefixChangeReasons[0] != "tools" {
		t.Fatalf("second change reasons = %v, want [tools]", second.PrefixChangeReasons)
	}
	if second.CacheHitTokens != 80 || second.CacheMissTokens != 20 {
		t.Fatalf("second cache tokens = hit %d miss %d, want hit 80 miss 20", second.CacheHitTokens, second.CacheMissTokens)
	}
	if first.ToolsHash == second.ToolsHash {
		t.Fatalf("tool hash should change after registering a tool: %q", first.ToolsHash)
	}
}

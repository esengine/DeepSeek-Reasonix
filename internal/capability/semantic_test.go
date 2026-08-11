package capability

import (
	"context"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
)

type fakeStreamProvider struct {
	chunks []provider.Chunk
}

func (f *fakeStreamProvider) Name() string { return "fake" }

func (f *fakeStreamProvider) Stream(_ context.Context, _ provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk, len(f.chunks))
	for _, c := range f.chunks {
		ch <- c
	}
	close(ch)
	return ch, nil
}

type captureSink struct{ events []event.Event }

func (c *captureSink) Emit(e event.Event) { c.events = append(c.events, e) }

func TestSemanticRouterRecordsPricedUsage(t *testing.T) {
	prov := &fakeStreamProvider{chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: `["skill:review"]`},
		{Type: provider.ChunkUsage, Usage: &provider.Usage{PromptTokens: 1000, CompletionTokens: 10}},
		{Type: provider.ChunkDone},
	}}
	audit := &Audit{}
	sink := &captureSink{}
	pricing := &provider.Pricing{Input: 1, Output: 2}
	r := &SemanticRouter{Provider: prov, Sink: sink, Model: "deepseek/deepseek-v4-flash", Pricing: pricing, Audit: audit}
	catalog := Catalog{Entries: []Entry{{
		ID: "skill:review", Kind: KindSkill, Name: "review",
		Description: "review code changes", Status: StatusReady,
	}}}

	decision := r.RouteSemantic(context.Background(), "please review the code", catalog, RouteDecision{})
	if len(decision.Candidates) == 0 {
		t.Fatalf("semantic route produced no candidates: %+v", decision)
	}
	snap := audit.Snapshot()
	if snap.RouterPromptTokens != 1000 || snap.RouterCompletionTokens != 10 {
		t.Fatalf("router token counters not recorded: prompt=%d completion=%d", snap.RouterPromptTokens, snap.RouterCompletionTokens)
	}
	if snap.RouterCost <= 0 {
		t.Fatalf("router cost must be priced, got %v", snap.RouterCost)
	}
	if snap.RouterLatencyMs < 0 {
		t.Fatalf("router latency negative: %v", snap.RouterLatencyMs)
	}
	var usageEvent *event.Event
	for _, e := range sink.events {
		if e.Kind == event.Usage && e.Pricing == pricing {
			copy := e
			usageEvent = &copy
		}
	}
	if usageEvent == nil {
		t.Fatalf("usage event missing Pricing: %+v", sink.events)
	}
	if usageEvent.ModelRef != "deepseek/deepseek-v4-flash" {
		t.Fatalf("usage event model ref = %q", usageEvent.ModelRef)
	}
}

func TestMergeSemanticIDsSkipsUnavailableEntries(t *testing.T) {
	catalog := Catalog{Entries: []Entry{
		{ID: "mcp-tool:failed", Kind: KindMCPTool, Status: StatusFailed},
		{ID: "mcp-server:disabled", Kind: KindMCPServer, Status: StatusDisabled},
		{ID: "skill:ready", Kind: KindSkill, Status: StatusReady},
	}}
	decision := mergeSemanticIDs(RouteDecision{}, catalog, []string{
		"mcp-tool:failed", "mcp-server:disabled", "skill:ready",
	}, "test")
	if len(decision.Candidates) != 1 || decision.Candidates[0].Entry.ID != "skill:ready" {
		t.Fatalf("semantic candidates = %+v, want only ready entry", decision.Candidates)
	}
}

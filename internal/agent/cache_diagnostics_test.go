package agent

import (
	"context"
	"sync/atomic"
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

// TestCacheMissDropFirstTurnSkips verifies the first turn (no previous baseline)
// never reports a miss drop regardless of the hit token count.
func TestCacheMissDropFirstTurnSkips(t *testing.T) {
	a := &Agent{lastResponseHitTokens: atomic.Int64{}}
	if detectResponseCacheMiss(a, 8000, nil) {
		t.Fatal("first turn should never report cache miss drop")
	}
	// Baseline must be stored.
	if a.lastResponseHitTokens.Load() != 8000 {
		t.Fatalf("baseline not stored: got %d", a.lastResponseHitTokens.Load())
	}
}

// TestCacheMissDropStableHitDoesNotFire verifies a stable or growing hit count
// (within 5%) does not trigger the drop detector.
func TestCacheMissDropStableHitDoesNotFire(t *testing.T) {
	a := &Agent{lastResponseHitTokens: atomic.Int64{}}
	a.lastResponseHitTokens.Store(10000)
	if detectResponseCacheMiss(a, 10100, nil) {
		t.Fatal("+1% change should not trigger flush")
	}
	if detectResponseCacheMiss(a, 9600, nil) {
		t.Fatal("4% drop should not trigger flush (<5% threshold)")
	}
}

// TestCacheMissDropSignificantDropFires verifies a drop >5% AND >=2000 tokens
// triggers the missed-cache detection.
func TestCacheMissDropSignificantDropFires(t *testing.T) {
	a := &Agent{lastResponseHitTokens: atomic.Int64{}}
	a.lastResponseHitTokens.Store(10000)
	if !detectResponseCacheMiss(a, 7900, nil) {
		t.Fatal("21% drop of 10000 → 7900 should trigger miss; threshold is 5% + 2000 tokens")
	}
	// Verify baseline was updated to the new (lower) count so a second
	// identical drop does not re-trigger.
	if detectResponseCacheMiss(a, 7900, nil) {
		t.Fatal("baseline not updated — second identical count should not re-trigger")
	}
}

// TestCacheMissDropCompactionResets verifies that contentReasons (compaction/snip)
// suppress the drop detector because the prefix legitimately shrank.
func TestCacheMissDropCompactionResets(t *testing.T) {
	a := &Agent{lastResponseHitTokens: atomic.Int64{}}
	a.lastResponseHitTokens.Store(10000)
	reasons := []string{"compact_auto"}
	if detectResponseCacheMiss(a, 5000, reasons) {
		t.Fatal("compaction-caused hit drop must not be reported as cache miss")
	}
	// Baseline must be reset to the post-compaction count so the next
	// legitimate cache eviction can still be detected.
	if a.lastResponseHitTokens.Load() != 5000 {
		t.Fatalf("baseline not reset after compaction: got %d", a.lastResponseHitTokens.Load())
	}
}

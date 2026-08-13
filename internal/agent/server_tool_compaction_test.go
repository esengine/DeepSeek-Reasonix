package agent

import (
	"context"
	"errors"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

type repairCompactionProvider struct {
	requests      []provider.Request
	searchSummary bool
	armPressure   *Agent
}

func (*repairCompactionProvider) Name() string                    { return "deepseek" }
func (*repairCompactionProvider) RequiresToolCallReasoning() bool { return true }

func (p *repairCompactionProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.requests = append(p.requests, freezeProviderRequest(req))
	if len(p.requests) == 1 && p.armPressure != nil {
		// The first model round must remain ordinary. Make only the following
		// finalization round cross the pressure threshold.
		p.armPressure.contextWindow = 5000
	}
	var chunks []provider.Chunk
	switch {
	case len(req.Messages) > 0 && req.Messages[0].Content == summarySystemPrompt:
		if p.searchSummary {
			chunks = []provider.Chunk{{Type: provider.ChunkServerSearch, ServerSearch: &provider.ServerSearchCall{ID: "summary-search"}}}
		} else {
			chunks = []provider.Chunk{{Type: provider.ChunkText, Text: "safe compact summary"}, {Type: provider.ChunkDone}}
		}
	case len(p.requests) == 1:
		chunks = reasoningOnlyStop("work is complete")
	default:
		chunks = []provider.Chunk{{Type: provider.ChunkText, Text: "visible result"}, {Type: provider.ChunkDone}}
	}
	out := make(chan provider.Chunk, len(chunks))
	for _, chunk := range chunks {
		out <- chunk
	}
	close(out)
	return out, nil
}

func newRepairCompactionRunAgent(t *testing.T, p *repairCompactionProvider) *Agent {
	t.Helper()
	a := New(p, tool.NewRegistry(), foldableSessionOverForce(6), Options{
		CompactRatio: 0.5, CompactForceRatio: 0.5,
		RecentKeep: 2, ArchiveDir: t.TempDir(),
	}, event.Discard)
	p.armPressure = a
	return a
}

func isSummaryRequest(req provider.Request) bool {
	return len(req.Messages) > 0 && req.Messages[0].Content == summarySystemPrompt
}

func TestRunScopedVisibleFinalRepairCompactsWithServerToolsDisabled(t *testing.T) {
	p := &repairCompactionProvider{}
	a := newRepairCompactionRunAgent(t, p)

	if err := a.Run(WithRequireVisibleFinal(context.Background()), "finish visibly"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(p.requests) != 3 {
		t.Fatalf("requests = %d, want initial, summary, repair", len(p.requests))
	}
	if p.requests[0].DisableServerTools || isSummaryRequest(p.requests[0]) {
		t.Fatalf("initial request unexpectedly restricted or summarized: %+v", p.requests[0])
	}
	if !isSummaryRequest(p.requests[1]) || !p.requests[1].DisableServerTools {
		t.Fatalf("pressure summary did not suppress server tools: %+v", p.requests[1])
	}
	if isSummaryRequest(p.requests[2]) || !p.requests[2].DisableServerTools {
		t.Fatalf("visible repair request = %+v, want non-summary with server tools disabled", p.requests[2])
	}
}

func TestRunScopedVisibleFinalRepairCompactionSearchStopsBeforeRepair(t *testing.T) {
	p := &repairCompactionProvider{searchSummary: true}
	a := newRepairCompactionRunAgent(t, p)
	before := a.currentProjectionVersion()

	err := a.Run(WithRequireVisibleFinal(context.Background()), "finish visibly")
	if !errors.Is(err, errServerToolDuringFinalization) {
		t.Fatalf("Run error = %v, want server-tool policy failure", err)
	}
	if len(p.requests) != 2 || !isSummaryRequest(p.requests[1]) || !p.requests[1].DisableServerTools {
		t.Fatalf("requests = %+v, want initial then disabled summary and no repair request", p.requests)
	}
	if got := a.currentProjectionVersion(); got != before {
		t.Fatalf("projection version = %d, want unchanged %d", got, before)
	}
	for _, msg := range a.Session().Snapshot() {
		if len(msg.ResponsesItems) > 0 || len(msg.ServerSearch) > 0 {
			t.Fatalf("failed summary search persisted: %+v", msg)
		}
	}
}

func newRepairCompactionAgent(t *testing.T, p *repairCompactionProvider) *Agent {
	t.Helper()
	return New(p, tool.NewRegistry(), foldableSessionOverForce(6), Options{
		ContextWindow: 5000, CompactRatio: 0.5, CompactForceRatio: 0.5,
		RecentKeep: 2, ArchiveDir: t.TempDir(),
	}, event.Discard)
}

func TestVisibleFinalRepairCompactionSuppressesServerTools(t *testing.T) {
	p := &repairCompactionProvider{}
	a := newRepairCompactionAgent(t, p)
	a.turn.visibleFinal.repairing = true
	if _, err := a.contextManager().Prepare(context.Background(), ContextPreparePolicy{Trigger: CompactionTriggerOverflow, Force: true}); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if len(p.requests) != 1 || !p.requests[0].DisableServerTools {
		t.Fatalf("repair summary requests = %+v", p.requests)
	}
	prepared, err := a.prepareSamplingRequest(context.Background())
	if err != nil {
		t.Fatalf("prepareSamplingRequest: %v", err)
	}
	suppressServerToolsForVisibleFinalRepair(&a.turn, &prepared.req)
	if !prepared.req.DisableServerTools {
		t.Fatal("repair sampling request exposed provider server tools")
	}
}

func TestVisibleFinalRepairCompactionSearchFailsClosed(t *testing.T) {
	p := &repairCompactionProvider{searchSummary: true}
	a := newRepairCompactionAgent(t, p)
	a.turn.visibleFinal.repairing = true
	before := a.currentProjectionVersion()
	_, err := a.contextManager().Prepare(context.Background(), ContextPreparePolicy{Trigger: CompactionTriggerOverflow, Force: true})
	if !errors.Is(err, errServerToolDuringFinalization) {
		t.Fatalf("Prepare error = %v, want server-tool policy failure", err)
	}
	if len(p.requests) != 1 || !p.requests[0].DisableServerTools {
		t.Fatalf("requests = %+v, want one disabled summary only", p.requests)
	}
	if got := a.currentProjectionVersion(); got != before {
		t.Fatalf("projection version = %d, want unchanged %d", got, before)
	}
	for _, msg := range a.Session().Snapshot() {
		if len(msg.ResponsesItems) > 0 || len(msg.ServerSearch) > 0 {
			t.Fatalf("failed summary search persisted: %+v", msg)
		}
	}
}

func TestOrdinaryCompactionKeepsServerToolsEnabled(t *testing.T) {
	p := &repairCompactionProvider{}
	a := newRepairCompactionAgent(t, p)
	a.turn.visibleFinal.repairing = false
	if _, err := a.contextManager().Prepare(context.Background(), ContextPreparePolicy{Trigger: CompactionTriggerOverflow, Force: true}); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if len(p.requests) != 1 || p.requests[0].DisableServerTools {
		t.Fatalf("ordinary summary requests = %+v", p.requests)
	}
}

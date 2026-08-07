package agent

import (
	"context"
	"testing"

	"reasonix/internal/ablation"
	"reasonix/internal/evidence"
	"reasonix/internal/provider"
)

func TestEvidenceAblationStandsDownTheReadinessGate(t *testing.T) {
	writer := evidence.Receipt{ToolName: "write_file", Success: true, Write: true, Paths: []string{"a.go"}}
	todo := evidence.Receipt{ToolName: "todo_write", Success: true, Todos: []evidence.TodoItem{{Content: "edit", Status: "in_progress"}}}

	gated := &Agent{evidence: readinessLedger(writer, todo)}
	if gated.finalReadinessCheckFor().reason == "" {
		t.Fatal("control arm must still gate an incomplete todo after a write")
	}

	off := &Agent{evidence: readinessLedger(writer, todo), ablation: ablation.New(ablation.Evidence)}
	if got := off.finalReadinessCheckFor().reason; got != "" {
		t.Fatalf("evidence ablation still gated the final answer: %q", got)
	}

	unrelated := &Agent{evidence: readinessLedger(writer, todo), ablation: ablation.New(ablation.Planner)}
	if unrelated.finalReadinessCheckFor().reason == "" {
		t.Fatal("an unrelated ablation must not disable the readiness gate")
	}
}

func TestCompactionAblationCollapsesTheCachePreservingDeferral(t *testing.T) {
	full := &Agent{contextWindow: 100_000, softCompactRatio: 0.5, toolResultSnipRatio: 0.7, compactRatio: 0.8}
	soft, snip, high := full.compactThresholds()
	if soft != 50_000 || snip != 70_000 || high != 80_000 {
		t.Fatalf("control thresholds = %d/%d/%d, want 50000/70000/80000", soft, snip, high)
	}

	off := &Agent{contextWindow: 100_000, softCompactRatio: 0.5, toolResultSnipRatio: 0.7, compactRatio: 0.8,
		ablation: ablation.New(ablation.Compaction)}
	soft, snip, high = off.compactThresholds()
	if soft != 50_000 || snip != soft || high != soft {
		t.Fatalf("ablated thresholds = %d/%d/%d, want all three at 50000", soft, snip, high)
	}
}

func TestForceThresholdReservesOutputBudget(t *testing.T) {
	// 1M window, 0.9 ratio: naive force = 900K, but the 128K output budget
	// leaves only 917K-8K input allowance. The force mark must shrink.
	a := &Agent{contextWindow: 1_048_576, compactForceRatio: 0.9, outputBudget: 131_072}
	got := a.forceThreshold()
	if want := 1_048_576 - 131_072 - 8192; got != want {
		t.Fatalf("forceThreshold = %d, want %d (window minus output budget minus reserve)", got, want)
	}
	// A request at the threshold + output budget must fit inside the window.
	if got+131_072 >= a.contextWindow {
		t.Fatalf("threshold %d + budget %d >= window %d: request would be rejected", got, 131_072, a.contextWindow)
	}
	// Without a provider budget the legacy ratio stands.
	b := &Agent{contextWindow: 1_048_576, compactForceRatio: 0.9}
	if got := b.forceThreshold(); got != 943_718 {
		t.Fatalf("no-budget forceThreshold = %d, want 943718", got)
	}
	// A small budget that stays under the ratio must not be overridden.
	c := &Agent{contextWindow: 1_048_576, compactForceRatio: 0.5, outputBudget: 131_072}
	if got := c.forceThreshold(); got != 524_288 {
		t.Fatalf("small-budget forceThreshold = %d, want 524288", got)
	}
}

func TestOutputBudgetOfNilAndUnawareProviders(t *testing.T) {
	if got := outputBudgetOf(nil); got != 0 {
		t.Fatalf("outputBudgetOf(nil) = %d, want 0", got)
	}
	// A provider that does not implement OutputBudgetProvider yields 0.
	np := &budgetlessProvider{}
	if got := outputBudgetOf(np); got != 0 {
		t.Fatalf("outputBudgetOf(budgetless) = %d, want 0", got)
	}
}

type budgetlessProvider struct{}

func (*budgetlessProvider) Name() string { return "budgetless" }
func (*budgetlessProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	return nil, nil
}

var _ provider.Provider = (*budgetlessProvider)(nil)

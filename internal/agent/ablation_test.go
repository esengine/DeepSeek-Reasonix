package agent

import (
	"testing"

	"reasonix/internal/ablation"
	"reasonix/internal/evidence"
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

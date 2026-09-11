package agent

import (
	"testing"

	"reasonix/internal/ablation"
	"reasonix/internal/evidence"
)

func TestEvidenceAblationStandsDownTheReadinessGate(t *testing.T) {
	writer := evidence.Receipt{ToolName: "write_file", Success: true, Write: true, Paths: []string{"a.go"}}
	todo := evidence.Receipt{ToolName: "todo_write", Success: true, Todos: []evidence.TodoItem{{Content: "edit", Status: "in_progress"}}}

	gated := &Agent{task: taskRuntime{ledger: readinessLedger(writer, todo)}, turn: turnRuntime{deliveryScopeActive: true}}
	if gated.finalReadinessCheckFor().reason == "" {
		t.Fatal("control arm must still gate an incomplete todo after a write")
	}

	off := &Agent{task: taskRuntime{ledger: readinessLedger(writer, todo)}, turn: turnRuntime{deliveryScopeActive: true}, ablation: ablation.New(ablation.Evidence)}
	if got := off.finalReadinessCheckFor().reason; got != "" {
		t.Fatalf("evidence ablation still gated the final answer: %q", got)
	}

	unrelated := &Agent{task: taskRuntime{ledger: readinessLedger(writer, todo)}, turn: turnRuntime{deliveryScopeActive: true}, ablation: ablation.New(ablation.Planner)}
	if unrelated.finalReadinessCheckFor().reason == "" {
		t.Fatal("an unrelated ablation must not disable the readiness gate")
	}
}

func TestCompactionAblationCollapsesTheCachePreservingDeferral(t *testing.T) {
	full := &Agent{agentConfig: agentConfig{contextWindow: 100_000, compactRatio: 0.8}}
	if got := full.compactTrigger(); got != 80_000 {
		t.Fatalf("control trigger = %d, want 80000", got)
	}

	off := &Agent{agentConfig: agentConfig{contextWindow: 100_000, compactRatio: 0.8}, ablation: ablation.New(ablation.Compaction)}
	// Compaction ablation forces the sole trigger down to 50% so folds fire earlier.
	if got := off.compactTrigger(); got != 50_000 {
		t.Fatalf("ablated trigger = %d, want 50000", got)
	}
}

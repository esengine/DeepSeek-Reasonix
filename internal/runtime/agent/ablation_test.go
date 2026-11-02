package agent

import (
	"testing"

	"reasonix/internal/contract/ablation"
)

func TestCompactionAblationCollapsesTheCachePreservingDeferral(t *testing.T) {
	full := &Agent{agentConfig: agentConfig{contextWindow: 100_000, compactRatio: 0.8}}
	if got := full.window().compactTrigger(); got != 80_000 {
		t.Fatalf("control trigger = %d, want 80000", got)
	}

	off := &Agent{agentConfig: agentConfig{contextWindow: 100_000, compactRatio: 0.8}, ablation: ablation.New(ablation.Compaction)}
	// Compaction ablation forces the sole trigger down to 50% so folds fire earlier.
	if got := off.window().compactTrigger(); got != 50_000 {
		t.Fatalf("ablated trigger = %d, want 50000", got)
	}
}

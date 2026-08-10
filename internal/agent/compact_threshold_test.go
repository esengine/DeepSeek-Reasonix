package agent

import "testing"

// An output budget larger than the window it shares (DeepSeek defaults to 128K,
// and 128000 was the value the custom-provider wizard wrote unconditionally)
// used to drive the hard ceiling to its one-token floor, so every turn crossed
// every trigger and compacted.
func TestCompactThresholdsSurviveOversizedOutputBudget(t *testing.T) {
	a := &Agent{
		prov:                &sharedWindowTestProvider{budget: 131_072, shared: true},
		contextWindow:       128_000,
		outputBudgetState:   outputBudgetState{outputBudget: 131_072},
		softCompactRatio:    defaultSoftCompactRatio,
		toolResultSnipRatio: defaultToolResultSnipRatio,
		compactRatio:        defaultCompactRatio,
		compactForceRatio:   defaultCompactForceRatio,
	}
	soft, snip, fold := a.compactThresholds()
	force := a.forceCompactThreshold(fold)
	// reserve = min(131072, 0.25*128000) = 32000; hard = 128000 - 32000 - 256.
	if soft != 64_000 || snip != 76_800 || fold != 95_744 || force != 95_744 {
		t.Fatalf("thresholds = %d/%d/%d/%d, want 64000/76800/95744/95744", soft, snip, fold, force)
	}
	if !(soft < snip && snip < fold) {
		t.Fatalf("thresholds collapsed onto each other: soft=%d snip=%d fold=%d", soft, snip, fold)
	}
}

// A declared window that genuinely fits its output budget keeps reserving the
// whole budget: the clamp must not loosen the ceiling for correct setups.
func TestCompactThresholdsReserveWholeBudgetWhenWindowFits(t *testing.T) {
	a := &Agent{
		prov:                &sharedWindowTestProvider{budget: 131_072, shared: true},
		contextWindow:       1_000_000,
		outputBudgetState:   outputBudgetState{outputBudget: 131_072},
		softCompactRatio:    defaultSoftCompactRatio,
		toolResultSnipRatio: defaultToolResultSnipRatio,
		compactRatio:        defaultCompactRatio,
		compactForceRatio:   defaultCompactForceRatio,
	}
	soft, snip, fold := a.compactThresholds()
	force := a.forceCompactThreshold(fold)
	if soft != 500_000 || snip != 600_000 || fold != 800_000 || force != 868_672 {
		t.Fatalf("thresholds = %d/%d/%d/%d, want 500000/600000/800000/868672", soft, snip, fold, force)
	}
}

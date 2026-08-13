package agent

import "testing"

// An output budget larger than the window it shares must never change the
// sole compact_ratio trigger. Output is clipped only at send time.
func TestCompactTriggerIndependentOfOutputBudget(t *testing.T) {
	a := &Agent{
		svc:         agentServices{prov: &sharedWindowTestProvider{budget: 131_072, shared: true}},
		agentConfig: agentConfig{contextWindow: 128_000, compactRatio: defaultCompactRatio},
		sess:        sessionRuntime{output: outputBudgetState{outputBudget: 131_072}},
	}
	trigger := a.compactTrigger()
	want := int(float64(128_000) * defaultCompactRatio)
	if trigger != want {
		t.Fatalf("trigger = %d, want %d (compact_ratio only)", trigger, want)
	}
	// Oversized output budget must not lower the trigger.
	a.sess.output.outputBudget = 200_000
	if got := a.compactTrigger(); got != want {
		t.Fatalf("trigger after larger output budget = %d, want %d", got, want)
	}
	hard := a.hardInputCeiling()
	if hard != 128_000-protocolReserveTokens {
		t.Fatalf("hard ceiling = %d, want window-protocolReserve", hard)
	}
}

func TestRecentTailBudgetClamp(t *testing.T) {
	cases := []struct {
		window int
		want   int
	}{
		{128_000, minRecentTailTokens},   // 10% = 12.8K → clamp to 32K
		{400_000, 40_000},                // 10% = 40K
		{1_000_000, maxRecentTailTokens}, // 10% = 100K → clamp to 96K
		{2_000_000, maxRecentTailTokens}, // still 96K
	}
	for _, tc := range cases {
		a := &Agent{agentConfig: agentConfig{contextWindow: tc.window, compactRatio: defaultCompactRatio}}
		if got := a.recentTailBudget(); got != tc.want {
			t.Fatalf("window %d: recentTailBudget = %d, want %d", tc.window, got, tc.want)
		}
	}
}

func TestCheckpointCeilingAndExceptionalSavings(t *testing.T) {
	a := &Agent{agentConfig: agentConfig{contextWindow: 1_000_000, compactRatio: 0.85}}
	if got := a.checkpointCeiling(); got != 500_000 {
		t.Fatalf("checkpointCeiling = %d, want 500000", got)
	}
	if got := a.exceptionalMinimumSavings(); got != 250_000 {
		t.Fatalf("exceptionalMinimumSavings = %d, want 250000", got)
	}
	if got := a.compactTrigger(); got != 850_000 {
		t.Fatalf("compactTrigger = %d, want 850000", got)
	}
}

func TestAcceptCheckpointCandidateRules(t *testing.T) {
	a := &Agent{agentConfig: agentConfig{contextWindow: 1_000_000, compactRatio: 0.85}}
	// 20% candidate under normal path: accept.
	if err := a.acceptCheckpointCandidate(CompactionTriggerPressure, false, 850_000, 180_000, 50_000); err != nil {
		t.Fatalf("20%% candidate: %v", err)
	}
	// 51% candidate: reject.
	if err := a.acceptCheckpointCandidate(CompactionTriggerPressure, false, 850_000, 510_000, 50_000); err == nil {
		t.Fatal("51% candidate should be rejected")
	}
	// Fixed prefix > 50% with enough savings: accept.
	if err := a.acceptCheckpointCandidate(CompactionTriggerPressure, false, 900_000, 600_000, 520_000); err != nil {
		t.Fatalf("fixed-prefix exception: %v", err)
	}
	// Fixed prefix > 50% without 25% savings: reject.
	if err := a.acceptCheckpointCandidate(CompactionTriggerPressure, false, 600_000, 550_000, 520_000); err == nil {
		t.Fatal("fixed-prefix without savings should reject")
	}
	// Manual below trigger: accept any reduction.
	if err := a.acceptCheckpointCandidate(CompactionTriggerManual, false, 100_000, 80_000, 10_000); err != nil {
		t.Fatalf("manual below trigger: %v", err)
	}
}

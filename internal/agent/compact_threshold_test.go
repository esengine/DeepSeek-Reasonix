package agent

import (
	"strings"
	"testing"

	"reasonix/internal/provider"
)

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

func TestRecentTailBudgetTracksCompactRatio(t *testing.T) {
	cases := []struct {
		name        string
		window      int
		compactRatio float64
		want        int
	}{
		// default 0.80 keeps the historic 16% of the window.
		{"default-80pct-10k", 10_000, defaultCompactRatio, 1_600},
		{"default-80pct-128k", 128_000, defaultCompactRatio, 20_480},
		{"default-80pct-400k", 400_000, defaultCompactRatio, 64_000},
		{"default-80pct-1m", 1_000_000, defaultCompactRatio, 160_000},
		// Lowering the threshold shrinks the retained tail proportionally.
		{"30pct-1m", 1_000_000, 0.30, 60_000},
		{"85pct-1m", 1_000_000, 0.85, 170_000},
		{"30pct-128k", 128_000, 0.30, 7_680},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := &Agent{agentConfig: agentConfig{contextWindow: tc.window, compactRatio: tc.compactRatio}}
			if got := a.recentTailBudget(); got != tc.want {
				t.Fatalf("window %d ratio %.2f: recentTailBudget = %d, want %d", tc.window, tc.compactRatio, got, tc.want)
			}
		})
	}
}

func TestDefaultCompactRatioIsEightyPercent(t *testing.T) {
	if defaultCompactRatio != 0.80 {
		t.Fatalf("defaultCompactRatio = %v, want 0.80", defaultCompactRatio)
	}
}

func TestMinRecentTailBudgetPreservesRecentTurns(t *testing.T) {
	// A very large recent user+assistant round must be fully covered by the
	// verbatim-tail floor so a low compact threshold never folds it away.
	big := strings.Repeat("x", 100_000)
	msgs := []provider.Message{
		{Role: provider.RoleUser, Content: big},
		{Role: provider.RoleAssistant, Content: "reply"},
	}
	a := &Agent{agentConfig: agentConfig{contextWindow: 1_000_000, compactRatio: 0.30}}
	got := a.minRecentTailBudget(msgs)
	if got <= 0 {
		t.Fatalf("minRecentTailBudget = %d, want > 0", got)
	}
	// The floor must not be smaller than the token estimate of the newest turn.
	need := int(float64(msgChars(msgs[0])) * a.tokPerChar())
	if got < need {
		t.Fatalf("minRecentTailBudget = %d, want >= %d covering the recent turn", got, need)
	}
}

func TestDeprecatedRetentionWarningOnlyForNonDefaultValues(t *testing.T) {
	if deprecatedContextRetentionConfigured(Options{}) ||
		deprecatedContextRetentionConfigured(Options{RecentKeep: 2, KeepPolicy: KeepErrors}) ||
		deprecatedContextRetentionConfigured(Options{RecentKeep: 2, KeepPolicy: KeepErrors | KeepUserMarked}) {
		t.Fatal("default compatibility values must not warn")
	}
	if !deprecatedContextRetentionConfigured(Options{RecentKeep: 7}) {
		t.Fatal("non-default recent_keep must warn")
	}
	if !deprecatedContextRetentionConfigured(Options{KeepPolicy: KeepUserMarked}) {
		t.Fatal("non-default keep policy must warn")
	}
}

func TestExplicitLegacyCompactRatioStillApplies(t *testing.T) {
	a := &Agent{agentConfig: agentConfig{contextWindow: 1_000_000, compactRatio: 0.85}}
	if got := a.compactTrigger(); got != 850_000 {
		t.Fatalf("compactTrigger = %d, want 850000", got)
	}
}

func TestAcceptCheckpointCandidateRules(t *testing.T) {
	a := &Agent{agentConfig: agentConfig{contextWindow: 1_000_000, compactRatio: 0.85}}
	// 20% candidate under normal path: accept.
	if err := a.acceptCheckpointCandidate(CompactionTriggerPressure, 850_000, 180_000); err != nil {
		t.Fatalf("20%% candidate: %v", err)
	}
	// Any strictly smaller candidate below the physical ceiling is accepted;
	// pressure convergence may summarize it once more.
	if err := a.acceptCheckpointCandidate(CompactionTriggerPressure, 850_000, 510_000); err != nil {
		t.Fatalf("51%% reducing candidate: %v", err)
	}
	// Large candidates remain valid while they shrink and stay below hard.
	if err := a.acceptCheckpointCandidate(CompactionTriggerPressure, 900_000, 600_000); err != nil {
		t.Fatalf("large reducing candidate: %v", err)
	}
	// Small but real savings are valid too.
	if err := a.acceptCheckpointCandidate(CompactionTriggerPressure, 600_000, 550_000); err != nil {
		t.Fatalf("small reducing candidate: %v", err)
	}
	// Manual below trigger: accept any reduction.
	if err := a.acceptCheckpointCandidate(CompactionTriggerManual, 100_000, 80_000); err != nil {
		t.Fatalf("manual below trigger: %v", err)
	}
}

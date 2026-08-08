package agent

import (
	"strings"
	"testing"

	"reasonix/internal/provider"
)

// TestEstimatedPromptTokensSafetyFactor verifies the no-usage conservative
// factor scales only the CJK share: the fixed estimator under-counts CJK
// transcripts (~1.8x measured), so the overflow guards must see the
// CJK-scaled value while English/code stays unscaled.
func TestEstimatedPromptTokensSafetyFactor(t *testing.T) {
	a := &Agent{}
	msgs := []provider.Message{{Role: provider.RoleUser, Content: "中文内容" + "的重复内容测试"}}
	est := a.estimatedPromptTokens(msgs)
	raw := estimateMessagesTokens(provider.ModelMessages(msgs))
	want := raw + cjkRunesInMessages(msgs)
	if est != want {
		t.Fatalf("estimatedPromptTokens = %d, want raw %d + cjk %d", est, raw, cjkRunesInMessages(msgs))
	}
	if est <= 0 {
		t.Fatal("estimate must stay positive")
	}
}

// TestEstimatedPromptTokensEnglishColdStartNotScaled guards the reported
// regression: an English/code session at ~40% real occupancy must not be
// doubled into the compaction threshold on a cold start (no usage yet).
func TestEstimatedPromptTokensEnglishColdStartNotScaled(t *testing.T) {
	a := &Agent{}
	msgs := []provider.Message{{Role: provider.RoleUser, Content: strings.Repeat("the quick brown fox ", 200)}}
	est := a.estimatedPromptTokens(msgs)
	raw := estimateMessagesTokens(provider.ModelMessages(msgs))
	if est != raw {
		t.Fatalf("English cold-start estimate = %d, want raw %d (no 2x)", est, raw)
	}
}

// TestEstimatedPromptTokensMixedScalesByCJKShare verifies a mixed transcript
// scales only its CJK portion, landing between ×1 and ×2.
func TestEstimatedPromptTokensMixedScalesByCJKShare(t *testing.T) {
	a := &Agent{}
	msgs := []provider.Message{
		{Role: provider.RoleUser, Content: strings.Repeat("english code text ", 100)},
		{Role: provider.RoleAssistant, Content: strings.Repeat("中文回复内容", 100)},
	}
	est := a.estimatedPromptTokens(msgs)
	raw := estimateMessagesTokens(provider.ModelMessages(msgs))
	if est <= raw {
		t.Fatalf("mixed estimate %d must exceed raw %d (CJK under-count)", est, raw)
	}
	if est >= raw*2 {
		t.Fatalf("mixed estimate %d must stay below raw×2 %d (English unscaled)", est, raw*2)
	}
}

// TestEstimatedPromptTokensCalibratesWithUsage verifies tokPerChar calibration
// replaces the safety factor once a turn reports real usage.
func TestEstimatedPromptTokensCalibratesWithUsage(t *testing.T) {
	sess := NewSession("sys")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "中文测试"})
	a := &Agent{session: sess}
	msgs := []provider.Message{{Role: provider.RoleUser, Content: "中文测试"}}
	raw := estimateMessagesTokens(provider.ModelMessages(msgs))
	a.lastUsage.Store(&provider.Usage{PromptTokens: 8})
	calibrated := a.estimatedPromptTokens(msgs)
	if calibrated <= raw {
		t.Fatalf("calibrated = %d must exceed raw %d (CJK under-count)", calibrated, raw)
	}
}

// TestEstimatedPromptTokensEmptyIsZero guards the zero-input edge.
func TestEstimatedPromptTokensEmptyIsZero(t *testing.T) {
	a := &Agent{}
	if got := a.estimatedPromptTokens(nil); got != 0 {
		t.Fatalf("empty estimate = %d, want 0", got)
	}
}

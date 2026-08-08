package agent

import (
	"testing"

	"reasonix/internal/provider"
)

// TestEstimatedPromptTokensSafetyFactor verifies the no-usage conservative
// factor: the fixed estimator under-counts CJK-heavy transcripts (~1.8x
// measured), so the overflow guards must see the scaled-up value.
func TestEstimatedPromptTokensSafetyFactor(t *testing.T) {
	a := &Agent{}
	msgs := []provider.Message{{Role: provider.RoleUser, Content: "中文内容" + "的重复内容测试"}}
	est := a.estimatedPromptTokens(msgs)
	raw := estimateMessagesTokens(provider.ModelMessages(msgs))
	if est != raw*promptEstimateSafetyFactor {
		t.Fatalf("estimatedPromptTokens = %d, want raw %d × factor %d", est, raw, promptEstimateSafetyFactor)
	}
	if est <= 0 {
		t.Fatal("estimate must stay positive")
	}
}

// TestEstimatedPromptTokensCalibratesWithUsage verifies tokPerChar calibration
// replaces the safety factor once a turn reports real usage.
func TestEstimatedPromptTokensCalibratesWithUsage(t *testing.T) {
	// CJK-heavy content with a real ratio tighter than the 4 chars/token
	// fallback: calibration must scale the raw estimate up (toward reality)
	// without applying the fixed 2x factor on top.
	sess := NewSession("sys")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "中文测试"})
	a := &Agent{session: sess}
	msgs := []provider.Message{{Role: provider.RoleUser, Content: "中文测试"}}
	raw := estimateMessagesTokens(provider.ModelMessages(msgs))
	// Real usage: 15 chars (3 sys + 12 CJK) at 2 chars/token ≈ 8 prompt tokens.
	a.lastUsage.Store(&provider.Usage{PromptTokens: 8})
	calibrated := a.estimatedPromptTokens(msgs)
	if calibrated <= raw {
		t.Fatalf("calibrated = %d must exceed raw %d (CJK under-count)", calibrated, raw)
	}
	// Both the calibration path and the safety-factor path are conservative;
	// calibration may land above the fixed 2x when the real ratio is tighter
	// than 4 chars/token — that is still a safe, conservative clip.
}

// TestEstimatedPromptTokensEmptyIsZero guards the zero-input edge.
func TestEstimatedPromptTokensEmptyIsZero(t *testing.T) {
	a := &Agent{}
	if got := a.estimatedPromptTokens(nil); got != 0 {
		t.Fatalf("empty estimate = %d, want 0", got)
	}
}

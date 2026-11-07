package agent

import (
	"strings"
	"testing"

	"reasonix/internal/base/i18n"
)

// A fold archives to a file and no summary is written, so no reason a fold was
// declined may say one is being paid for.
func TestDeclineWordingNamesNoSummary(t *testing.T) {
	for _, reason := range []CompactionNoopReason{NoopFoldBelowEconomics, NoopInputUnchanged, NoopNoNewClosedPrefix} {
		if text := CompactDeclineText(reason); strings.Contains(strings.ToLower(text), "summar") {
			t.Errorf("%s: %q mentions a summary", reason, text)
		}
	}
	if text := i18n.M.CompactionWhy["fold_below_economics"]; text == "" || strings.Contains(strings.ToLower(text), "summar") || strings.Contains(text, "摘要") {
		t.Errorf("localized wording = %q", text)
	}
}

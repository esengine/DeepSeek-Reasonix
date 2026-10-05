package agent

import (
	"strings"
	"testing"

	"reasonix/internal/contract/provider"
)

// TestFormatUsageLineSurfacesMultiRequestTurns pins the reading fix: the input
// column is a billable sum across attempts, so a turn that replayed once must
// say so instead of looking like a context that doubled.
func TestFormatUsageLineSurfacesMultiRequestTurns(t *testing.T) {
	replayed := &provider.Usage{
		TotalTokens: 20194, PromptTokens: 20088, CompletionTokens: 106,
		CacheHitTokens: 19712, CacheMissTokens: 376,
		ContextPromptTokens: 10044, RequestCount: 2,
	}
	line := FormatUsageLine(replayed, nil, nil)
	if !strings.Contains(line, "2 requests, context 10044") {
		t.Fatalf("multi-request turn hides its replay: %q", line)
	}

	single := &provider.Usage{
		TotalTokens: 10100, PromptTokens: 10044, CompletionTokens: 56,
		CacheHitTokens: 9984, CacheMissTokens: 60,
		ContextPromptTokens: 10044, RequestCount: 1,
	}
	if line := FormatUsageLine(single, nil, nil); strings.Contains(line, "requests") {
		t.Fatalf("ordinary turn gained noise: %q", line)
	}
}

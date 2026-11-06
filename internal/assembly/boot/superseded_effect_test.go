package boot

import (
	"reasonix/internal/contract/provider"
)

// isSummarizerRequest tells the compaction summarizer's own call apart from the
// agent's. Only the agent's requests carry the projection under test.
func isSummarizerRequest(req provider.Request) bool {
	return req.Summary
}

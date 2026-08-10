package responses

import (
	"context"

	"reasonix/internal/provider"
)

// CompactionCapabilities reports model-level compaction budgets from the
// vendor capability table. Unknown endpoints report zero budgets so agents
// do not inherit a large default merely for being OpenAI-compatible.
func (c *client) CompactionCapabilities() provider.CompactionCapabilities {
	if c == nil {
		return provider.CompactionCapabilities{}
	}
	return provider.CompactionCapabilities{
		NativeCompaction:       c.caps.nativeCompaction,
		MaxOutputTokens:        c.caps.defaultMaxOutputTokens,
		CompactionOutputTokens: c.caps.compactionOutputTokens,
	}
}

// Compact implements provider.NativeCompactor. Responses vendors do not yet
// expose a dedicated compact endpoint, so this always returns
// ErrCompactionUnsupported and the agent falls back to ordinary summarize.
func (c *client) Compact(ctx context.Context, req provider.CompactionRequest) (provider.CompactionResult, error) {
	_ = ctx
	_ = req
	return provider.CompactionResult{}, provider.ErrCompactionUnsupported
}

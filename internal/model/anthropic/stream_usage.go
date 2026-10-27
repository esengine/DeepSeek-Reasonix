package anthropic

import (
	"context"

	"reasonix/internal/contract/provider"
)

// streamUsage owns cumulative counters for one HTTP response. An interrupted
// response retains known counts without certifying that its bill is complete.
type streamUsage struct {
	input, output, cacheCreate, cacheRead, serverTools int
	have, attempted                                    bool
}

func (u *streamUsage) merge(w *wireUsage) {
	if w == nil {
		return
	}
	// Native input/cache counts arrive in message_start; some gateways send
	// everything in message_delta. Repeated cumulative counts are not additive.
	u.input = max(u.input, w.InputTokens)
	u.output = max(u.output, w.OutputTokens)
	u.cacheCreate = max(u.cacheCreate, w.CacheCreationInputTokens)
	u.cacheRead = max(u.cacheRead, w.CacheReadInputTokens)
	u.serverTools = max(u.serverTools, w.ServerToolUse.WebSearchRequests)
	u.have = true
}

func (u *streamUsage) finish(ctx context.Context, native bool, reason string, partial bool, send func(provider.Chunk) bool) bool {
	if u.attempted {
		return true
	}
	u.attempted = true
	if !u.have {
		return true
	}
	writeBilled := 0.0
	if u.cacheCreate > 0 && native {
		writeBilled = float64(u.cacheCreate) * cacheWrite5MinuteInputMultiplier
	}
	usage := &provider.Usage{
		PromptTokens:           u.input + u.cacheCreate + u.cacheRead,
		CompletionTokens:       u.output,
		TotalTokens:            u.input + u.cacheCreate + u.cacheRead + u.output,
		CacheHitTokens:         u.cacheRead,
		CacheMissTokens:        u.input + u.cacheCreate,
		CacheWriteTokens:       u.cacheCreate,
		CacheWriteBilledTokens: writeBilled,
		ServerToolRequests:     u.serverTools,
		FinishReason:           mapStopReason(reason),
		Estimated:              partial,
	}
	if partial {
		usage.FinishReason = "interrupted"
	}
	provider.ApplyRequestAttemptCount(ctx, usage)
	return send(provider.Chunk{Type: provider.ChunkUsage, Usage: usage})
}

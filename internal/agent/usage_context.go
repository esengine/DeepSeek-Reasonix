package agent

import "reasonix/internal/provider"

// applyLatestContextShape copies the latest single-request shape into Context*
// fields for gauges and Desktop rebind telemetry.
func applyLatestContextShape(dst, latest *provider.Usage) {
	if dst == nil || latest == nil {
		return
	}
	dst.ContextPromptTokens = latest.PromptTokens
	dst.ContextCompletionTokens = latest.CompletionTokens
	dst.ContextReasoningTokens = latest.ReasoningTokens
	dst.ContextCacheHitTokens = latest.CacheHitTokens
	dst.ContextCacheMissTokens = latest.CacheMissTokens
}

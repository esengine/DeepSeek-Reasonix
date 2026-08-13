package agent

import (
	"encoding/json"

	"reasonix/internal/event"
	"reasonix/internal/provider"
)

// estimateFailedAttemptUsage fills Estimated usage when a body attempt ends
// without a terminal provider usage record, so billing and observational Goal
// usage still include the issued request plus any observed speculative output.
// Non-interrupt failures that already carry usage (e.g. client reasoning limit)
// are left intact.
//
// httpRequests is the SendWithRetry attempt-counter delta for this body attempt.
// When it is 0 and there was no speculative output, the failure was local or
// came from a provider without observable transport accounting; return nil or
// its existing usage rather than inventing billable tokens.
func estimateFailedAttemptUsage(usage *provider.Usage, frozen samplingRequest, result streamedTurn, httpRequests int) *provider.Usage {
	if result.err == nil {
		return usage
	}
	// Preserve exact client-side finish reasons that already computed usage.
	if usage != nil && usage.FinishReason != "" && usage.FinishReason != "interrupted" {
		return usage
	}
	// A zero-output, non-interrupted failure with no observed HTTP request is a
	// local/provider validation failure. It is not a billable sampling attempt.
	preBodyLocal := httpRequests <= 0 && !result.interrupted &&
		!provider.IsStreamInterrupted(result.err) && !sawSpeculativeSamplingOutput(result)
	if preBodyLocal {
		if usage != nil && usageTotalTokens(usage) > 0 {
			return usage
		}
		return nil
	}
	if !provider.IsStreamInterrupted(result.err) && !result.interrupted {
		// Auth/cancel/decode/limit paths keep their own accounting.
		if usage != nil {
			return usage
		}
		if httpRequests <= 0 {
			return nil
		}
	}
	textBytes := len(result.text)
	reasoningBytes := len(result.reasoning)
	maxArg := result.maxArgChars
	for _, call := range result.partialCalls {
		if n := len(call.Arguments); n > maxArg {
			maxArg = n
		}
	}
	for _, call := range result.calls {
		if n := len(call.Arguments); n > maxArg {
			maxArg = n
		}
	}
	if usage != nil && !usage.Estimated && usage.TotalTokens > 0 {
		return usage
	}
	finish := "interrupted"
	if usage != nil && usage.FinishReason != "" {
		finish = usage.FinishReason
	}
	est := bestEffortStreamUsage(usage, textBytes, reasoningBytes, finish)
	if est == nil {
		est = &provider.Usage{Estimated: true, FinishReason: finish}
	}
	if est.PromptTokens <= 0 {
		est.PromptTokens = estimateSamplingRequestInputTokens(frozen.req)
		est.Estimated = true
	}
	// Estimated failed attempts without cache split still need Cost() to see
	// billable input — Price falls back to PromptTokens only when hit+miss=0.
	if est.CacheHitTokens+est.CacheMissTokens == 0 && est.PromptTokens > 0 {
		est.CacheMissTokens = est.PromptTokens
	}
	if maxArg > 0 {
		argTokens := (maxArg + 3) / 4
		if est.CompletionTokens < argTokens+estimateTokensFromBytes(textBytes)+estimateTokensFromBytes(reasoningBytes) {
			est.CompletionTokens = argTokens + estimateTokensFromBytes(textBytes) + estimateTokensFromBytes(reasoningBytes)
			est.Estimated = true
		}
	}
	if minTotal := est.PromptTokens + est.CompletionTokens; est.TotalTokens < minTotal {
		est.TotalTokens = minTotal
		est.Estimated = true
	}
	return est
}

func sawSpeculativeSamplingOutput(result streamedTurn) bool {
	return result.text != "" || result.reasoning != "" || result.maxArgChars > 0 ||
		result.partialToolStarted || len(result.calls) > 0 || len(result.partialCalls) > 0
}

// estimateSamplingRequestInputTokens reconstructs a conservative input count
// only when an interrupted attempt closed before terminal provider usage. It is
// accounting telemetry, not request admission: the estimate never changes the
// frozen provider request or imposes a token ceiling.
func estimateSamplingRequestInputTokens(req provider.Request) int {
	total := 3
	for _, msg := range provider.ModelMessages(req.Messages) {
		total += 4
		total += estimateTextTokens(msg.Content)
		total += estimateTextTokens(msg.ReasoningContent)
		total += estimateTextTokens(msg.ReasoningSignature)
		total += estimateTextTokens(msg.Name)
		total += estimateTextTokens(msg.ToolCallID)
		for _, image := range msg.Images {
			total += estimateTextTokens(image)
		}
		for _, call := range msg.ToolCalls {
			total += 8 + estimateTextTokens(call.ID) + estimateTextTokens(call.Name) + estimateTextTokens(call.Arguments)
		}
		for _, item := range msg.ResponsesItems {
			total += estimateTextTokens(string(item))
		}
		for _, search := range msg.ServerSearch {
			provider.WalkServerSearchEstimate(search, func(s string) {
				total += estimateTextTokens(s)
			})
		}
	}
	for _, schema := range req.Tools {
		encoded, _ := json.Marshal(schema)
		total += 8 + estimateTextTokens(string(encoded))
	}
	return max(total, 1)
}

// mergeSamplingUsage accumulates billable counters across body attempts.
// PromptTokens is the billable input total (aligned with cache hit+miss).
// ContextPromptTokens is set later by finalizeSamplingUsage from the latest attempt.
func mergeSamplingUsage(acc, attempt *provider.Usage) *provider.Usage {
	if attempt == nil {
		return acc
	}
	billableHitMiss := func(u *provider.Usage) (hit, miss int) {
		if u == nil {
			return 0, 0
		}
		if u.CacheHitTokens+u.CacheMissTokens > 0 {
			return u.CacheHitTokens, u.CacheMissTokens
		}
		// No cache split: treat PromptTokens as uncached billable input.
		return 0, u.PromptTokens
	}
	billablePrompt := func(hit, miss, prompt int) int {
		if hit+miss > 0 {
			return hit + miss
		}
		return prompt
	}
	if acc == nil {
		merged := *attempt
		if merged.RequestCount <= 0 {
			merged.RequestCount = 1
		}
		hit, miss := billableHitMiss(attempt)
		merged.CacheHitTokens = hit
		merged.CacheMissTokens = miss
		merged.PromptTokens = billablePrompt(hit, miss, attempt.PromptTokens)
		return &merged
	}
	merged := *acc
	// Billable input for Cost: sum hit/miss (prompt when no cache split).
	ah, am := billableHitMiss(acc)
	bh, bm := billableHitMiss(attempt)
	// If acc was previously merged, CacheHit+Miss already holds the sum and
	// PromptTokens may still be the first attempt's value — prefer stored sums.
	if acc.CacheHitTokens+acc.CacheMissTokens > 0 {
		ah, am = acc.CacheHitTokens, acc.CacheMissTokens
	}
	merged.CacheHitTokens = ah + bh
	merged.CacheMissTokens = am + bm
	merged.CacheWriteTokens += attempt.CacheWriteTokens
	merged.CacheWriteBilledTokens += attempt.CacheWriteBilledTokens
	merged.PromptTokens = billablePrompt(merged.CacheHitTokens, merged.CacheMissTokens, 0)
	if merged.PromptTokens == 0 {
		merged.PromptTokens = acc.PromptTokens + attempt.PromptTokens
	}
	merged.CompletionTokens += attempt.CompletionTokens
	merged.ReasoningTokens += attempt.ReasoningTokens
	merged.TotalTokens += usageTotalTokens(attempt)
	merged.RequestCount = usageRequestCount(acc) + usageRequestCount(attempt)
	if attempt.Estimated {
		merged.Estimated = true
	}
	if attempt.FinishReason != "" {
		merged.FinishReason = attempt.FinishReason
	}
	return &merged
}

// storeLatestRequestUsage records single-request usage, never a billable aggregate.
func (a *Agent) storeLatestRequestUsage(attempt *provider.Usage) {
	if a == nil || attempt == nil {
		return
	}
	// Skip request-only shells with no token shape.
	if attempt.PromptTokens <= 0 && attempt.CompletionTokens <= 0 && attempt.TotalTokens <= 0 {
		return
	}
	clone := *attempt
	// Keep the per-attempt RequestCount; context calculations do not use it.
	a.sess.output.lastUsage.Store(&clone)
	a.setPromptTokenCalibrationFromUsage(&clone)
}

// finalizeSamplingUsage builds the Usage event payload for consumers that
// expect one coherent billable record:
//   - PromptTokens / cache hit+miss / Completion / Total / RequestCount: billable aggregate
//   - Context* fields: latest attempt only (context gauges + rebind telemetry)
func finalizeSamplingUsage(billable, latest *provider.Usage) *provider.Usage {
	if billable == nil && latest == nil {
		return nil
	}
	if billable == nil {
		out := *latest
		applyLatestContextShape(&out, latest)
		return &out
	}
	out := *billable
	if latest != nil {
		applyLatestContextShape(&out, latest)
		out.FinishReason = latest.FinishReason
	}
	// Ensure PromptTokens matches billable input (hit+miss) for CLI/ACP/Desktop
	// telemetry that requires cache totals to align with PromptTokens.
	if hitMiss := out.CacheHitTokens + out.CacheMissTokens; hitMiss > 0 {
		out.PromptTokens = hitMiss
	}
	if out.TotalTokens < out.PromptTokens+out.CompletionTokens {
		out.TotalTokens = out.PromptTokens + out.CompletionTokens
	}
	return &out
}

// mergeStreamUsage remains for missing-reasoning style single-repair merges that
// need a simple sum. Sampling recovery uses mergeSamplingUsage instead.
func mergeStreamUsage(first, retry *provider.Usage) *provider.Usage {
	return mergeSamplingUsage(first, retry)
}

func usageTotalTokens(u *provider.Usage) int {
	if u == nil {
		return 0
	}
	if u.TotalTokens > 0 {
		return u.TotalTokens
	}
	return u.PromptTokens + u.CompletionTokens
}

func usageRequestCount(usage *provider.Usage) int {
	if usage == nil {
		return 0
	}
	if usage.RequestCount > 0 {
		return usage.RequestCount
	}
	return 1
}

func (a *Agent) emitTurnUsage(usage *provider.Usage, cacheDiagnostics *CacheDiagnostics) {
	if usage == nil || (usage.TotalTokens <= 0 && usage.RequestCount <= 0) {
		return
	}
	// lastUsage must stay as the latest single-request shape (set during
	// sampling recovery). Never overwrite it with a multi-attempt billable
	// aggregate — that would inflate ContextSnapshot and compaction decisions.
	if a.sess.output.lastUsage.Load() == nil && usage.PromptTokens > 0 {
		a.storeLatestRequestUsage(usage)
	}
	a.svc.sink.Emit(event.Event{Kind: event.Usage, ModelRef: a.modelRef, Usage: usage, Pricing: a.svc.pricing,
		UsageSource:      a.usageSource,
		CacheDiagnostics: cacheDiagnostics,
		SessionHit:       int(a.sess.cacheHit.Load()), SessionMiss: int(a.sess.cacheMiss.Load())})
}

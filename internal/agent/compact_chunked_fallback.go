package agent

import (
	"context"
	"fmt"

	"reasonix/internal/provider"
)

// foldSummaryWithChunkedFallback retries summary size failures through the
// resilient fragment/tree-reduce path used for over-length sessions. It lives
// in its own file so growth here cannot push compact_projection.go past its
// repolint ceiling, and an upstream convergence replacing that file cannot
// silently drop this local guard (it exists upstream too, without the
// pre-flight below).
func (a *Agent) foldSummaryWithChunkedFallback(ctx context.Context, trigger string, prefix, fold []provider.Message, instructions string, sourceTokens int, inputMode string) (foldSummary, CompactionTelemetry, error) {
	// Pre-flight: a fold already over the safe prompt budget would 400 on the
	// provider — go straight to the fragmenting path instead of burning the
	// doomed single call. validateSafeSummaryRequest no-ops when enforcement
	// is off (window-independent gateways keep the original request shape).
	var res foldSummary
	var tele CompactionTelemetry
	var chunkedReason error
	preflight := a.validateSafeSummaryRequest(prefix, fold, instructions, false)
	if preflight == nil {
		var err error
		res, tele, err = a.foldSummaryWithTelemetry(ctx, trigger, prefix, fold, instructions, sourceTokens, inputMode)
		if err == nil || !chunkedFallbackApplies(err, inputMode) {
			return res, tele, err
		}
		chunkedReason = err
	} else if len(fold) == 0 {
		// Empty fold with an over-budget instruction set: single call is the
		// only shape (instructions ride the prefix), so surface the rejection.
		res, tele, err := a.foldSummaryWithTelemetry(ctx, trigger, prefix, fold, instructions, sourceTokens, inputMode)
		return res, tele, err
	} else {
		chunkedReason = preflight
	}
	// When foldExtra is nil (view replay fits), the full fold region is in prefix.
	chunkedInput := fold
	if len(chunkedInput) == 0 {
		chunkedInput = prefix
	}
	chunked, chunkedErr := a.chunkedFoldSummary(ctx, chunkedInput, instructions, nil)
	chunked.Usage = mergeSamplingUsage(res.Usage, chunked.Usage)
	chunked.Spans += res.Spans
	if chunked.FoldTokens <= 0 {
		chunked.FoldTokens = res.FoldTokens
	}
	if chunked.RequestID == "" {
		chunked.RequestID = res.RequestID
	}
	if chunkedErr != nil {
		tele = a.telemetryFromSummary(trigger, a.CacheState(), sourceTokens, chunked, nil, chunkedInput)
		tele.Error = fmt.Sprintf("%v (chunked fallback: %v)", chunkedReason, chunkedErr)
		return chunked, tele, chunkedErr
	}
	return chunked, a.telemetryFromSummary(trigger, a.CacheState(), sourceTokens, chunked, nil, chunkedInput), nil
}

// chunkedFallbackApplies reports a size failure the fragment path can fix. A
// provider overflow qualifies only once the transcript form has failed too;
// before that a re-planned replay is one request instead of many.
func chunkedFallbackApplies(err error, inputMode string) bool {
	if provider.AsContextLimitError(err) != nil {
		return inputMode == SummaryInputSlim
	}
	return summarySizeFailure(err)
}

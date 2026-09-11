package agent

import "reasonix/internal/provider"

// foldRequest is what one fold attempt asks of the summarizer. force shrinks
// the verbatim tail, mustFree caps the summary input to the safe prefix, and
// allowChunked permits the multi-request fragment path after a size failure.
type foldRequest struct {
	force, mustFree, allowChunked bool
	// slim renders the fold as one bounded transcript instead of the
	// cache-aligned replay. It is a rung on the overflow ladder, never a default.
	slim bool
}

// summaryInputModeFor labels the summarizer input for telemetry and the
// chunked-fallback gate. The slim rung overrides the replay-shape labels.
func summaryInputModeFor(req foldRequest, pinned, rewritten bool) string {
	switch {
	case req.slim:
		return SummaryInputSlim
	case pinned:
		return SummaryInputNonPrefix
	case rewritten:
		return SummaryInputExtensionRewritten
	default:
		return SummaryInputCachePrefix
	}
}

// Overflow ladder for one maintenance transaction: replay-form summaries first
// (each re-planned on the calibration a provider overflow just corrected),
// then one transcript-form summary, then the fragment path when allowed.
const maxSummaryReplans = 2

// summaryLadder paces the fold attempts of one maintenance transaction and
// absorbs provider overflows by moving to the next rung instead of failing.
type summaryLadder struct {
	remaining int // successful summaries still allowed by the trigger's policy
	replans   int // overflow re-plans consumed
	slim      bool
}

func newSummaryLadder(maxSummaries int) *summaryLadder {
	return &summaryLadder{remaining: maxSummaries}
}

// next reports whether another fold attempt may start and consumes one slot.
func (l *summaryLadder) next() bool {
	if l.remaining <= 0 {
		return false
	}
	l.remaining--
	return true
}

func (l *summaryLadder) request(force, mustFree, allowChunked bool) foldRequest {
	return foldRequest{force: force, mustFree: mustFree, allowChunked: allowChunked, slim: l.slim}
}

// absorbOverflow moves to the next rung after a provider overflow and returns
// the slot it consumed, so the caller retries without spending a summary. A
// re-plan is only worth a request when the reply carried the prompt count
// that recalibrates it; otherwise the transcript form is the next rung.
func (l *summaryLadder) absorbOverflow(err error) bool {
	limit := provider.AsContextLimitError(err)
	if limit == nil {
		return false
	}
	switch {
	case limit.PromptTokens > 0 && l.replans < maxSummaryReplans && !l.slim:
		l.replans++
	case !l.slim:
		l.slim = true
	default:
		return false
	}
	l.remaining++
	return true
}

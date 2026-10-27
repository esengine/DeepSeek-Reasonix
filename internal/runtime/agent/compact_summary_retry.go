package agent

import (
	"context"
	"errors"
	"fmt"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
)

// summaryTruncation is errSummaryOutputTruncated plus what was spent: the
// output cap of the attempt that was cut and whether a larger one was tried.
type summaryTruncation struct {
	Cap     int
	Retried bool
}

func (e *summaryTruncation) Error() string {
	if e.Retried {
		return fmt.Sprintf("%v: provider reached the output token limit (%d tokens) after one retry with a larger budget", errSummaryOutputTruncated, e.Cap)
	}
	return fmt.Sprintf("%v: provider reached the output token limit (%d tokens)", errSummaryOutputTruncated, e.Cap)
}

func (e *summaryTruncation) Unwrap() error { return errSummaryOutputTruncated }

// summaryResetBackoff is the wait before the one retry of a summary whose
// stream the peer reset.
var summaryResetBackoff = 2 * time.Second

var errSummaryPrefixNotDigest = fmt.Errorf("reused prefix: %w", errSummaryNotDigest)

// A summary gets at most one independent-shape fallback, one output-cap retry
// and one retry after the peer reset its stream.
// Every attempt records its own spend; the returned usage is their aggregate.
func (a *contextWindow) summarizeOnce(ctx context.Context, fold []provider.Message, instructions string) (string, *provider.Usage, error) {
	allowPrefix, retried, reset := true, false, false
	limit, above := summaryOutputMaxTokens, 0
	var total *provider.Usage
	for {
		summary, usage, err := a.summarizeAtMode(ctx, fold, instructions, limit, above, allowPrefix)
		total = mergeSamplingUsage(total, usage)
		if ctx.Err() != nil {
			return summary, total, err
		}
		if !reset && provider.StreamInterruptReason(err) == provider.StreamInterruptConnectionReset {
			reset = true
			if !sleepUnlessDone(ctx, summaryResetBackoff) {
				return summary, total, err
			}
			a.svc.sink.Emit(event.Event{Kind: event.CompactionProgress})
			continue
		}
		if allowPrefix && (errors.Is(err, errSummaryPrefixNotDigest) || errors.Is(err, provider.ErrSummaryToolsUnsupported)) {
			allowPrefix = false
		} else {
			var cut *summaryTruncation
			if !errors.As(err, &cut) {
				return summary, total, err
			}
			if retried {
				cut.Retried = true
				return summary, total, err
			}
			// Do not erase the truncation if the larger cap cannot fit the context.
			next, nextUsage, nextErr := a.retryTruncatedSummary(ctx, fold, instructions, cut, allowPrefix)
			total = mergeSamplingUsage(total, nextUsage)
			if errors.Is(nextErr, errNoOutputHeadroom) {
				return summary, total, err
			}
			if ctx.Err() != nil {
				return next, total, nextErr
			}
			var nextCut *summaryTruncation
			if errors.As(nextErr, &nextCut) {
				nextCut.Retried = true
			}
			if !allowPrefix || (!errors.Is(nextErr, errSummaryPrefixNotDigest) && !errors.Is(nextErr, provider.ErrSummaryToolsUnsupported)) {
				return next, total, nextErr
			}
			allowPrefix, retried = false, true
			limit, above = summaryRetryOutputMaxTokens, cut.Cap
		}
		a.svc.sink.Emit(event.Event{Kind: event.CompactionProgress})
	}
}

func (a *contextWindow) retryTruncatedSummary(ctx context.Context, fold []provider.Message, instructions string, cut *summaryTruncation, allowPrefix bool) (string, *provider.Usage, error) {
	a.svc.sink.Emit(event.Event{Kind: event.CompactionProgress})
	return a.summarizeAtMode(ctx, fold, instructions, summaryRetryOutputMaxTokens, cut.Cap, allowPrefix)
}

// sleepUnlessDone waits d and reports whether it ran to the end: false when ctx
// ended first.
func sleepUnlessDone(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

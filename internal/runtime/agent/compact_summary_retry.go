package agent

import (
	"context"
	"errors"
	"fmt"

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

// summarizeOnce performs one summary request. Only output truncation earns a
// second, and only one: the host knows the cap was the cause, and the request
// is byte-identical but for a larger cap, so its input stays cache-warm.
// Timeouts, empty results and stream errors fail once with no second attempt.
func (a *contextWindow) summarizeOnce(ctx context.Context, fold []provider.Message, instructions string) (string, *provider.Usage, error) {
	summary, usage, err := a.summarize(ctx, fold, instructions)
	var cut *summaryTruncation
	if !errors.As(err, &cut) || cut.Retried || ctx.Err() != nil {
		return summary, usage, err
	}
	// The cut digest has already streamed to the card; the retry writes a new one.
	a.svc.sink.Emit(event.Event{Kind: event.CompactionProgress})
	summary2, usage2, err2 := a.summarizeAt(ctx, fold, instructions, summaryRetryOutputMaxTokens, cut.Cap)
	if errors.Is(err2, errNoOutputHeadroom) {
		return summary, usage, err
	}
	var cut2 *summaryTruncation
	if errors.As(err2, &cut2) {
		cut2.Retried = true
	}
	return summary2, usage2, err2
}

package agent

import (
	"context"

	"reasonix/internal/provider"
)

const minSummaryOutputTokens = 512

// summaryOutputBudget scales only shared/unknown-window summaries. Providers
// with an independent completion window keep the full digest cap; smaller
// shared windows reserve one quarter for a useful briefing without crowding
// every fold out of the prompt budget.
func (a *Agent) summaryOutputBudget() int {
	if contextBudgetPolicyOf(a.svc.prov).WindowMode == provider.ContextWindowIndependent {
		return summaryOutputMaxTokens
	}
	window := a.effectiveContextWindow()
	if window <= 0 {
		return summaryOutputMaxTokens
	}
	return min(summaryOutputMaxTokens, max(window/4, minSummaryOutputTokens))
}

// foldSummary is what compaction reports about turning a fold into a digest.
// It is populated even when the call fails, so telemetry still records how
// large the attempt was and that exactly one call was used.
type foldSummary struct {
	Text       string
	Mode       string
	RequestID  string
	Usage      *provider.Usage
	FoldTokens int
	Spans      int
	InputMode  string
}

func summaryInputTokens(msgs []provider.Message) int {
	return estimateMessagesTokens(msgs)
}

func (a *Agent) guardedSummaryInputTokens(msgs []provider.Message) int {
	return a.estimatedVisibleRequestTokens(msgs)
}

func (a *Agent) summaryInputBudget(instructions string) int {
	window := a.effectiveContextWindow()
	if window <= 0 {
		window = a.contextWindow
	}
	if window <= 0 {
		return 0
	}
	return max(0, window-a.summaryOutputBudget()-estimateTextTokens(compactionInstruction)-estimateTextTokens(instructions)-protocolReserveTokens)
}

// foldToSummary turns a fold region into one digest with exactly one provider
// request. Pressure-time tool pruning is durable and happens before this call;
// the summary request never performs a private second transformation.
func (a *Agent) foldToSummary(ctx context.Context, fold []provider.Message, instructions string) (foldSummary, error) {
	return a.foldToSummaryMode(ctx, fold, instructions, SummaryInputCachePrefix)
}

func (a *Agent) foldToSummaryMode(ctx context.Context, fold []provider.Message, instructions, inputMode string) (foldSummary, error) {
	res := foldSummary{Mode: CompactionModeSummarized, Spans: 1, FoldTokens: summaryInputTokens(fold), InputMode: inputMode}
	return a.singleCallSummary(ctx, res, fold, instructions)
}

func (a *Agent) singleCallSummary(ctx context.Context, res foldSummary, fold []provider.Message, instructions string) (foldSummary, error) {
	summary, mode, usage, reqID, err := a.runCompactionSummary(ctx, fold, instructions)
	res.Text, res.Mode, res.Usage, res.RequestID = summary, mode, usage, reqID
	return res, err
}

func (a *Agent) foldSummaryWithTelemetry(ctx context.Context, trigger string, fold []provider.Message, instructions string, sourceTokens int, inputMode string) (foldSummary, CompactionTelemetry, error) {
	res, err := a.foldToSummaryMode(ctx, fold, instructions, inputMode)
	tele := compactionTelemetryFromSummary(trigger, a.CacheState(), sourceTokens, res)
	if err != nil {
		tele.Error = err.Error()
	}
	return res, tele, err
}

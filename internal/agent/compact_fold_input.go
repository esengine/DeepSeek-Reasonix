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
func (a *Agent) foldToSummary(ctx context.Context, prefix, fold []provider.Message, instructions string) (foldSummary, error) {
	return a.foldToSummaryMode(ctx, nil, fold, instructions, SummaryInputCachePrefix)
}

func (a *Agent) foldToSummaryMode(ctx context.Context, prefix, fold []provider.Message, instructions, inputMode string) (foldSummary, error) {
	res := foldSummary{Mode: CompactionModeSummarized, Spans: 1, FoldTokens: summaryInputTokens(fold), InputMode: inputMode}
	if inputMode == SummaryInputSlim {
		summary, usage, err := a.summarizeTranscript(ctx, fold, instructions)
		res.Text, res.Usage = summary, usage
		return res, err
	}
	return a.singleCallSummary(ctx, res, prefix, fold, instructions)
}

func (a *Agent) singleCallSummary(ctx context.Context, res foldSummary, prefix, fold []provider.Message, instructions string) (foldSummary, error) {
	summary, mode, usage, reqID, err := a.runCompactionSummary(ctx, prefix, fold, instructions)
	res.Text, res.Mode, res.Usage, res.RequestID = summary, mode, usage, reqID
	return res, err
}

// fillCompactionWireTelemetry attributes the summary request's actual wire
// shape: the fold-view fingerprint, the normalized bytes sent last request,
// and which tool set the summary carried. These let a system-only cache hit
// or a post-resume miss be traced to tool-seam or byte divergence.
func (a *Agent) fillCompactionWireTelemetry(tele *CompactionTelemetry, prefix, fold []provider.Message) {
	view := append(append([]provider.Message(nil), prefix...), fold...)
	tele.ViewFP = providerVisibleFingerprint(modelInputMessages(view))
	tele.WireFP = a.sess.wireFP()
	tele.PrefixHash = summarizePrefixHash(prefix)
	tele.PrefLen = len(prefix)
	if saved := a.savedMainRequest(); saved != nil {
		tele.WireLen = len(saved.messages)
		tele.WireDiff = firstWireDiff(saved.messages, prefix)
	}
	if schemas, source := a.summaryToolsSource(); source != "none" {
		tele.ToolsCount = len(schemas)
		tele.ToolsFP = toolsFingerprint(schemas)
		tele.ToolsSource = source
	}
}

// summarizePrefixHash fingerprints the summarizer's prefix so compaction passes
// can be compared for drift: a stable hash with a low hit rate means the bytes
// were never sent before; a changing hash means the boundary moved.
func summarizePrefixHash(prefix []provider.Message) string {
	h := providerVisibleFingerprint(provider.ModelMessages(prefix))
	if len(h) > 12 {
		h = h[:12]
	}
	return h
}

// telemetryFromSummary builds a compaction record for a fold region and
// attributes its wire shape in one call, so every emit site stays one line.
func (a *Agent) telemetryFromSummary(trigger, cacheState string, sourceTokens int, res foldSummary, prefix, fold []provider.Message) CompactionTelemetry {
	tele := compactionTelemetryFromSummary(trigger, cacheState, sourceTokens, res)
	a.fillCompactionWireTelemetry(&tele, prefix, fold)
	return tele
}

func (a *Agent) foldSummaryWithTelemetry(ctx context.Context, trigger string, prefix, fold []provider.Message, instructions string, sourceTokens int, inputMode string) (foldSummary, CompactionTelemetry, error) {
	res, err := a.foldToSummaryMode(ctx, prefix, fold, instructions, inputMode)
	tele := a.telemetryFromSummary(trigger, a.CacheState(), sourceTokens, res, prefix, fold)
	if err != nil {
		tele.Error = err.Error()
	}
	return res, tele, err
}

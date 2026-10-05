package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
)

// Summary input construction is internal to a summary transaction. The normal
// model-visible view is never rewritten by these helpers; only the temporary
// request fed to the summarizer is shortened.
const (
	summaryOutputReserve = summaryOutputMaxTokens // room reserved for the digest output
	minSummarySpanTokens = 4000                   // below this a fold is too small to summarize usefully
)

const (
	summaryTruncateMarker = "\n[... %d characters omitted; the original is retained in the canonical transcript ...]\n"
	summaryOmittedMessage = "[... %d messages of this part omitted to fit the summarizer; the originals are retained in the canonical transcript ...]"
)

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
	// Coverage is what this digest kept of the facts its fold produced.
	Coverage foldCoverage
	// CoverageBackstopped marks a digest the host had to complete itself.
	CoverageBackstopped bool
}

// summaryInputTokens sizes messages as summarizer input in the real tokens
// summaryInputBudget is expressed in; estimateTextTokens counts characters and
// would read this fold as four times its cost. The rendered transcript is what
// actually rides in the request, so its per-message framing is measured rather
// than assumed away.
func (a *contextWindow) summaryInputTokens(msgs []provider.Message) int {
	if len(msgs) == 0 {
		return 0
	}
	return a.estimatedPromptTokens([]provider.Message{{
		Role: provider.RoleUser, Content: renderTranscript(msgs),
	}})
}

// summaryInputBudget is the transcript ceiling for one summarizer call.
// Zero means the window cannot host a useful summary request.
func (a *contextWindow) summaryInputBudget(instructions string) int {
	window := a.effectiveContextWindow()
	if window <= 0 {
		return 0
	}
	reserve := summaryOutputReserve
	if sharesContextWindow(a.svc.prov) && a.configuredOutputBudget(a.maxOutputTokens) > 0 {
		reserve += outputBudgetReserve
	}
	framing := a.estimatedPromptTokens([]provider.Message{
		{Role: provider.RoleSystem, Content: summarySystemPrompt},
		{Role: provider.RoleUser, Content: instructions},
	})
	budget := window - reserve - framing - protocolReserveTokens
	if budget < minSummarySpanTokens {
		return 0
	}
	return budget
}

// foldToSummary turns a fold region into one digest with at most one provider
// request. Oversized input is shortened deterministically for the summarizer
// only; multi-span merge and application-layer retries are gone.
func (a *contextWindow) foldToSummary(ctx context.Context, fold []provider.Message, instructions string) (foldSummary, error) {
	res := foldSummary{Mode: CompactionModeSummarized, Spans: 1, FoldTokens: a.summaryInputTokens(fold)}
	budget := a.summaryInputBudget(instructions)
	if budget <= 0 {
		// No declared window (or unusable window): send one unbounded call.
		// Manual /compact on an unconfigured provider still works this way.
		return a.singleCallSummary(ctx, res, fold, instructions)
	}
	input := fold
	if res.FoldTokens > budget {
		input = a.shortenFoldForSummary(fold)
		res.FoldTokens = a.summaryInputTokens(input)
	}
	if res.FoldTokens > budget {
		input = a.compressFoldArgsForSummary(input)
		res.FoldTokens = a.summaryInputTokens(input)
	}
	if res.FoldTokens > budget {
		input = a.omitLowValueForSummary(input, budget)
		res.FoldTokens = a.summaryInputTokens(input)
	}
	if res.FoldTokens > budget {
		return res, fmt.Errorf("%w after shortening (%d > %d)", errSummaryInputTooLarge, res.FoldTokens, budget)
	}
	return a.singleCallSummary(ctx, res, input, instructions)
}

func (a *contextWindow) singleCallSummary(ctx context.Context, res foldSummary, fold []provider.Message, instructions string) (foldSummary, error) {
	summary, mode, usage, reqID, err := a.runCompactionSummary(ctx, fold, instructions)
	res.Text, res.Mode, res.Usage, res.RequestID = summary, mode, usage, reqID
	return res, err
}

// foldOrDegrade summarizes a fold and, when that fold is the only way out,
// converts a summarizer failure into a mechanical one. The telemetry it returns
// always reports the original failure, even when the fold recovered from it.
func (a *contextWindow) foldOrDegrade(ctx context.Context, trigger string, mustFree bool, fold []provider.Message, instructions string, sourceTokens int) (foldSummary, CompactionTelemetry, error) {
	spend := compactionSpendFrom(ctx)
	res, err := a.foldToSummary(ctx, fold, instructions)
	if err == nil {
		res.Coverage = measureFoldCoverage(fold, a.toolFactsFor, res.Text)
		if err = rejectDigestThatCarriedNothing(res, mustFree); err == nil {
			res = backstopFoldCoverage(res)
		}
		tele := compactionTelemetryFromSummary(trigger, a.cacheState(), sourceTokens, res, spend.read())
		return res, tele, err
	}
	tele := compactionTelemetryFromSummary(trigger, a.cacheState(), sourceTokens, res, spend.read())
	cause := err.Error()
	if res, err = a.degradeFoldSummary(res, mustFree, fold, err); err != nil {
		tele.Error = cause
		return res, tele, err
	}
	res = backstopFoldCoverage(res)
	tele = compactionTelemetryFromSummary(trigger, a.cacheState(), sourceTokens, res, spend.read())
	tele.Error = cause
	return res, tele, nil
}

// mechanicalFoldDigest stands in for a digest the summarizer could not produce.
// Saying the summary is missing is what stops the model reading the gap as
// "nothing was there" and inventing what the folded turns contained.
func mechanicalFoldDigest(n int) string {
	return fmt.Sprintf("%d earlier message(s) were folded here to free context, but the automatic summary was unavailable. Ask the user if you need details from before this point.", n)
}

// degradeFoldSummary turns a summarizer failure into a mechanical fold only
// where that fold is the only way out; below the ceiling the turn still goes
// out, so the error is kept and a recoverable view is not folded blind.
// Cancellation is the caller's decision, never a summarizer failure.
func (a *contextWindow) degradeFoldSummary(res foldSummary, mustFree bool, fold []provider.Message, cause error) (foldSummary, error) {
	if !mustFree || errors.Is(cause, context.Canceled) {
		return res, cause
	}
	a.svc.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn,
		Text:   "Context was compacted without a generated summary.",
		Detail: fmt.Sprintf("compaction summary unavailable (%v); folded mechanically", cause)})
	res.Text = mechanicalFoldDigest(len(fold))
	res.Mode = CompactionModeDegraded
	// Measured here too: without it a fold that lost everything reports the
	// same coverage as one that lost nothing.
	res.Coverage = measureFoldCoverage(fold, a.toolFactsFor, res.Text)
	return res, nil
}

// backstopFoldCoverage writes the facts the digest dropped into the digest
// itself. mustFree is why this exists rather than a second summarizer call:
// under hard pressure the repair path is skipped and the degraded path never
// had a digest to repair, and neither may cost the model a change it made.
func backstopFoldCoverage(res foldSummary) foldSummary {
	block := foldCoverageBackstop(res.Coverage)
	if block == "" {
		return res
	}
	res.Text = strings.TrimRight(res.Text, "\n") + "\n\n" + block
	res.CoverageBackstopped = true
	return res
}

// shortenFoldForSummary rewrites only summarizer input: long tool bodies become
// deterministic head+tail sketches. The body it sketches is the one the
// conversation carried — a sketch of the local full copy would hand the digest
// a tail the model was never given, and the digest becomes the memory of it.
func (a *contextWindow) shortenFoldForSummary(fold []provider.Message) []provider.Message {
	out := make([]provider.Message, len(fold))
	copy(out, fold)
	saved := 0
	for i, m := range out {
		if m.LocalOnly || m.Role != provider.RoleTool {
			continue
		}
		if a.keepPolicy&KeepErrors != 0 && isErrorMessage(m) {
			continue
		}
		if len(m.Content) < minPruneBytes {
			continue
		}
		replacement := snipToolResult(provider.Message{
			Role: m.Role, Name: m.Name, ToolCallID: m.ToolCallID, Content: m.Content,
		}, "the canonical transcript", a.snipStrategyFor(m.Name))
		if replacement == m.Content {
			continue
		}
		saved += len(m.Content) - len(replacement)
		out[i].Content = replacement
		out[i].RawContent = ""
	}
	if saved == 0 {
		return fold
	}
	return out
}

// compressFoldArgsForSummary reduces long tool-call argument payloads to key
// names and sizes so the summarizer request can fit.
func (a *contextWindow) compressFoldArgsForSummary(fold []provider.Message) []provider.Message {
	out := make([]provider.Message, len(fold))
	copy(out, fold)
	changed := false
	for i, m := range out {
		if m.Role != provider.RoleAssistant || len(m.ToolCalls) == 0 {
			continue
		}
		calls := make([]provider.ToolCall, len(m.ToolCalls))
		copy(calls, m.ToolCalls)
		for j, tc := range calls {
			if len(tc.Arguments) < 512 {
				continue
			}
			calls[j].Arguments = summarizeToolArgs(tc.Arguments)
			changed = true
		}
		out[i].ToolCalls = calls
	}
	if !changed {
		return fold
	}
	return out
}

// omitLowValueForSummary drops low-value middle tool results while keeping
// protected errors, existing digests, and the ends of the fold.
func (a *contextWindow) omitLowValueForSummary(fold []provider.Message, budget int) []provider.Message {
	if a.summaryInputTokens(fold) <= budget {
		return fold
	}
	marker := provider.Message{Role: provider.RoleUser, Content: fmt.Sprintf(summaryOmittedMessage, 0)}
	avail := budget - a.summaryInputTokens([]provider.Message{marker})
	if avail < minSummarySpanTokens/2 {
		return fold
	}
	var head, tail []provider.Message
	acc, i, j := 0, 0, len(fold)-1
	for i <= j {
		fromHead := len(head) <= len(tail)*2
		idx := j
		if fromHead {
			idx = i
		}
		// Prefer keeping digests and error tool results.
		m := fold[idx]
		cost := a.summaryInputTokens(fold[idx : idx+1])
		if acc+cost > avail && !(isCompactionSummary(m) || isErrorMessage(m)) {
			if fromHead {
				i++
			} else {
				j--
			}
			continue
		}
		if acc+cost > avail {
			break
		}
		acc += cost
		if fromHead {
			head = append(head, fold[i])
			i++
			continue
		}
		tail = append([]provider.Message{fold[j]}, tail...)
		j--
	}
	dropped := len(fold) - len(head) - len(tail)
	if dropped <= 0 || len(head)+len(tail) == 0 {
		return fold
	}
	marker.Content = fmt.Sprintf(summaryOmittedMessage, dropped)
	out := make([]provider.Message, 0, len(head)+len(tail)+1)
	out = append(out, head...)
	out = append(out, marker)
	return append(out, tail...)
}

// rejectDigestThatCarriedNothing refuses a checkpoint whose digest named none
// of the fold's changes. It judges the digest, not the completed text: the host
// block restores the facts, but accepting a summary that carried none of them
// because the host cleaned up would retire the one signal that says the
// summarizer produced nothing usable. A fold that must free is never refused.
func rejectDigestThatCarriedNothing(res foldSummary, mustFree bool) error {
	if mustFree || !res.Coverage.LostEveryChange() {
		return nil
	}
	return rejectCheckpoint(NoopDigestLostEveryChange, "the digest carried none of the fold's changes (%s)", res.Coverage.Reason())
}

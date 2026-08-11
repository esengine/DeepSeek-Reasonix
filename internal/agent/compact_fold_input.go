package agent

import (
	"context"
	"fmt"

	"reasonix/internal/provider"
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
}

// summaryInputTokens estimates what messages cost as summarizer input. The
// rendered transcript is what actually rides in the request, so its per-message
// framing is measured rather than assumed away.
func summaryInputTokens(msgs []provider.Message) int {
	return estimateTextTokens(renderTranscript(msgs))
}

// guardedSummaryInputTokens uses the same calibrated/conservative estimator as
// shared-window output clipping. Other providers retain the rendered-transcript
// estimate.
func (a *Agent) guardedSummaryInputTokens(msgs []provider.Message) int {
	raw := summaryInputTokens(msgs)
	if !sharesContextWindow(a.prov) || a.configuredOutputBudget(a.maxOutputTokens) <= 0 || len(msgs) == 0 {
		return raw
	}
	return a.estimatedPromptTokens([]provider.Message{{
		Role: provider.RoleUser, Content: renderTranscript(msgs),
	}})
}

// summaryInputBudget is the transcript ceiling for one summarizer call.
// Zero means the window cannot host a useful summary request.
func (a *Agent) summaryInputBudget(instructions string) int {
	if a.contextWindow <= 0 {
		return 0
	}
	reserve := summaryOutputReserve
	if sharesContextWindow(a.prov) && a.configuredOutputBudget(a.maxOutputTokens) > 0 {
		reserve += outputBudgetReserve
	}
	budget := a.contextWindow - reserve - estimateTextTokens(summarySystemPrompt) - estimateTextTokens(instructions) - 256
	if budget < minSummarySpanTokens {
		return 0
	}
	return budget
}

// foldToSummary turns a fold region into one digest with at most one provider
// request. Oversized input is shortened deterministically for the summarizer
// only; multi-span merge and application-layer retries are gone.
func (a *Agent) foldToSummary(ctx context.Context, fold []provider.Message, instructions string) (foldSummary, error) {
	res := foldSummary{Mode: CompactionModeSummarized, Spans: 1, FoldTokens: summaryInputTokens(fold)}
	budget := a.summaryInputBudget(instructions)
	if budget <= 0 {
		// No declared window (or unusable window): send one unbounded call.
		// Manual /compact on an unconfigured provider still works this way.
		return a.singleCallSummary(ctx, res, fold, instructions)
	}
	input := fold
	guardedTokens := a.guardedSummaryInputTokens(input)
	if guardedTokens > budget {
		input = a.shortenFoldForSummary(fold)
		res.FoldTokens = summaryInputTokens(input)
		guardedTokens = a.guardedSummaryInputTokens(input)
	}
	if guardedTokens > budget {
		input = a.compressFoldArgsForSummary(input)
		res.FoldTokens = summaryInputTokens(input)
		guardedTokens = a.guardedSummaryInputTokens(input)
	}
	if guardedTokens > budget {
		input = a.omitLowValueForSummary(input, budget)
		res.FoldTokens = summaryInputTokens(input)
		guardedTokens = a.guardedSummaryInputTokens(input)
	}
	if guardedTokens > budget {
		return res, fmt.Errorf("summary input still exceeds single-request budget after shortening (%d > %d)", guardedTokens, budget)
	}
	return a.singleCallSummary(ctx, res, input, instructions)
}

func (a *Agent) singleCallSummary(ctx context.Context, res foldSummary, fold []provider.Message, instructions string) (foldSummary, error) {
	summary, mode, usage, reqID, err := a.runCompactionSummary(ctx, fold, instructions)
	res.Text, res.Mode, res.Usage, res.RequestID = summary, mode, usage, reqID
	return res, err
}

// shortenFoldForSummary rewrites only summarizer input: long tool bodies become
// deterministic head+tail sketches. Prefer RawContent when present so the
// digest still sees the real tool outcome shape.
func (a *Agent) shortenFoldForSummary(fold []provider.Message) []provider.Message {
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
		source := m.Content
		if m.RawContent != "" {
			source = m.RawContent
		}
		if len(source) < minPruneBytes {
			continue
		}
		replacement := snipToolResult(provider.Message{
			Role: m.Role, Name: m.Name, ToolCallID: m.ToolCallID, Content: source,
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
func (a *Agent) compressFoldArgsForSummary(fold []provider.Message) []provider.Message {
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
func (a *Agent) omitLowValueForSummary(fold []provider.Message, budget int) []provider.Message {
	if summaryInputTokens(fold) <= budget {
		return fold
	}
	marker := provider.Message{Role: provider.RoleUser, Content: fmt.Sprintf(summaryOmittedMessage, 0)}
	avail := budget - summaryInputTokens([]provider.Message{marker})
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
		cost := summaryInputTokens(fold[idx : idx+1])
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

package agent

import (
	"context"
	"fmt"
	"strings"

	"reasonix/internal/event"
	"reasonix/internal/provider"
)

// Each fold re-derives its digest from the canonical transcript, so the region
// grows with the session while one summarizer call's window does not. These
// bound the call; the fold region itself stays whatever the plan selected.
const (
	summaryOutputReserve = 8192 // room the call still needs for the digest it must return
	minSummarySpanTokens = 4000 // below this a span is too small to summarize usefully
	maxSummarySpans      = 6    // ceiling on summarizer calls one fold may cost
)

const (
	summaryTruncateMarker = "\n[... %d characters omitted; the original is in the canonical transcript and the compaction archive ...]\n"
	summaryOmittedMessage = "[... %d messages of this part omitted to fit the summarizer; the originals are in the canonical transcript and the compaction archive ...]"
)

// mergeSpansInstruction steers the final pass when one fold needed several
// calls: the parts are one conversation in order, not competing summaries.
const mergeSpansInstruction = `The messages below are digests of consecutive parts of ONE conversation, in order. Merge them into a single briefing under the same headings. Keep every identifier, path, number, command outcome, and unresolved item; drop only duplication between parts.`

// foldSummary is what compaction reports about turning a fold into a digest.
// It is populated even when the call fails, so telemetry still records how
// large the attempt was and how many calls it took.
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
// shared-window output clipping. Other providers retain the existing rendered-
// transcript estimate and byte-identical single-call behavior.
func (a *Agent) guardedSummaryInputTokens(msgs []provider.Message) int {
	raw := summaryInputTokens(msgs)
	if !sharesContextWindow(a.prov) || a.configuredOutputBudget(a.maxOutputTokens) <= 0 || len(msgs) == 0 {
		return raw
	}
	return a.estimatedPromptTokens([]provider.Message{{
		Role: provider.RoleUser, Content: renderTranscript(msgs),
	}})
}

// summaryInputBudget is the transcript ceiling for one summarizer call: what
// the window has left once the digest, the summary prompt and the caller's
// instructions are reserved, since the provider counts all of them. Zero means
// unbounded — no declared window, or a window too small to leave a usable span,
// where shattering the fold would lose more than the failure it avoids.
func (a *Agent) summaryInputBudget(instructions string) int {
	if a.contextWindow <= 0 {
		return 0
	}
	reserve := summaryOutputReserve
	if sharesContextWindow(a.prov) && a.configuredOutputBudget(a.maxOutputTokens) > 0 {
		reserve += outputBudgetReserve
	}
	// Shared-window calls keep the digest allowance separate from the estimator
	// safety reserve. Independent-ceiling providers retain their existing budget.
	// Span passes add a per-part line to the instructions; 256 covers it.
	budget := a.contextWindow - reserve - estimateTextTokens(summarySystemPrompt) - estimateTextTokens(instructions) - 256
	if budget < minSummarySpanTokens {
		return 0
	}
	return budget
}

// foldToSummary turns a fold region into one digest, escalating only as far as
// the region's size demands: a fold that fits one call is summarized exactly as
// before, an oversized one first gives up the bulk of its stale tool results,
// and only a fold still too large is summarized in spans and merged.
func (a *Agent) foldToSummary(ctx context.Context, fold []provider.Message, instructions string) (foldSummary, error) {
	res := foldSummary{Mode: CompactionModeSummarized, Spans: 1, FoldTokens: summaryInputTokens(fold)}
	budget := a.summaryInputBudget(instructions)
	guardedTokens := a.guardedSummaryInputTokens(fold)
	if budget <= 0 || guardedTokens <= budget {
		return a.singleCallSummary(ctx, res, fold, instructions)
	}

	input := a.shortenFoldForSummary(fold)
	res.FoldTokens = summaryInputTokens(input)
	guardedTokens = a.guardedSummaryInputTokens(input)
	if guardedTokens <= budget {
		return a.singleCallSummary(ctx, res, input, instructions)
	}

	splitBudget := budget
	if guardedTokens > res.FoldTokens && guardedTokens > 0 {
		splitBudget = max(minSummarySpanTokens, budget*res.FoldTokens/guardedTokens)
	}
	spans := splitIntoSummarySpans(input, splitBudget)
	res.Spans = len(spans)
	if len(spans) == 1 {
		return a.singleCallSummary(ctx, res, spans[0], instructions)
	}
	a.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: fmt.Sprintf(
		"summarizing history in %d parts (~%d tokens est.) so no single summarizer call overflows", len(spans), res.FoldTokens)})
	summary, usage, err := a.summarizeSpans(ctx, spans, instructions)
	res.Text, res.Usage = summary, usage
	return res, err
}

func (a *Agent) singleCallSummary(ctx context.Context, res foldSummary, fold []provider.Message, instructions string) (foldSummary, error) {
	summary, mode, usage, reqID, err := a.runCompactionSummary(ctx, fold, instructions)
	res.Text, res.Mode, res.Usage, res.RequestID = summary, mode, usage, reqID
	return res, err
}

// shortenFoldForSummary trades bulk for reach when a fold outgrows one call:
// stale tool results are cut to their head and tail lines. Only the summarizer
// input is rewritten — the canonical transcript, the archived originals, and
// the messages kept verbatim in the projection are untouched.
func (a *Agent) shortenFoldForSummary(fold []provider.Message) []provider.Message {
	out := make([]provider.Message, len(fold))
	copy(out, fold)
	saved := 0
	for i, m := range out {
		if !shouldMaintainToolResult(m, toolResultSnip) {
			continue
		}
		if a.keepPolicy&KeepErrors != 0 && isErrorMessage(m) {
			continue
		}
		replacement := snipToolResult(m, "the compaction archive", a.snipStrategyFor(m.Name))
		if replacement == m.Content {
			continue
		}
		saved += len(m.Content) - len(replacement)
		out[i].Content = replacement
	}
	if saved == 0 {
		return fold
	}
	a.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: fmt.Sprintf(
		"shortened stale tool results in the compaction input (~%d chars) to fit the summarizer", saved)})
	return out
}

// summarizeSpans summarizes each span and merges the digests in a final pass.
// Every span is read from the canonical transcript, so this compresses breadth
// inside one fold — it never stacks a digest on top of an earlier digest.
func (a *Agent) summarizeSpans(ctx context.Context, spans [][]provider.Message, instructions string) (string, *provider.Usage, error) {
	var merged *provider.Usage
	digests := make([]string, 0, len(spans))
	for i, span := range spans {
		part := fmt.Sprintf("This is part %d of %d of the history being compacted; summarize only what this part contains.", i+1, len(spans))
		text, usage, err := a.summarizeWithRetry(ctx, span, joinSummaryInstructions(instructions, part))
		merged = mergeStreamUsage(merged, usage)
		if err != nil {
			return "", merged, fmt.Errorf("span %d of %d: %w", i+1, len(spans), err)
		}
		digests = append(digests, fmt.Sprintf("[part %d of %d]\n%s", i+1, len(spans), text))
	}
	final := []provider.Message{{Role: provider.RoleUser, Content: strings.Join(digests, "\n\n")}}
	text, usage, err := a.summarize(ctx, final, joinSummaryInstructions(instructions, mergeSpansInstruction))
	return text, mergeStreamUsage(merged, usage), err
}

// splitIntoSummarySpans cuts a fold into consecutive spans that each fit one
// summarizer call, capped at maxSummarySpans so a very long session cannot turn
// one compaction into an unbounded number of calls. A fold too large for that
// many full spans keeps each span's head and tail and records what it dropped.
func splitIntoSummarySpans(fold []provider.Message, budget int) [][]provider.Message {
	total := summaryInputTokens(fold)
	groups := groupByBudget(fold, max(budget, (total+maxSummarySpans-1)/maxSummarySpans))
	if len(groups) > maxSummarySpans {
		// Greedy packing can need one bin more than the arithmetic suggests.
		groups = coalesceGroups(groups, maxSummarySpans)
	}
	spans := make([][]provider.Message, 0, len(groups))
	for _, g := range groups {
		spans = append(spans, truncateSpanToBudget(g, budget))
	}
	if len(spans) == 0 {
		return [][]provider.Message{fold}
	}
	return spans
}

func groupByBudget(fold []provider.Message, budget int) [][]provider.Message {
	var groups [][]provider.Message
	var cur []provider.Message
	acc := 0
	for _, m := range fold {
		cost := summaryInputTokens([]provider.Message{m})
		if len(cur) > 0 && acc+cost > budget {
			groups = append(groups, cur)
			cur, acc = nil, 0
		}
		cur = append(cur, m)
		acc += cost
	}
	if len(cur) > 0 {
		groups = append(groups, cur)
	}
	return groups
}

// coalesceGroups merges consecutive groups into exactly n buckets of near-equal
// group count, preserving order.
func coalesceGroups(groups [][]provider.Message, n int) [][]provider.Message {
	out := make([][]provider.Message, 0, n)
	for i := range n {
		var merged []provider.Message
		for _, g := range groups[i*len(groups)/n : (i+1)*len(groups)/n] {
			merged = append(merged, g...)
		}
		if len(merged) > 0 {
			out = append(out, merged)
		}
	}
	return out
}

// truncateSpanToBudget keeps a span's leading and trailing messages and drops
// the middle, so a span the caller had to oversize still shows how its work
// began and where it ended up.
func truncateSpanToBudget(span []provider.Message, budget int) []provider.Message {
	if summaryInputTokens(span) <= budget {
		return span
	}
	if len(span) == 1 {
		return []provider.Message{truncateMessageToBudget(span[0], budget)}
	}
	marker := provider.Message{Role: provider.RoleUser, Content: fmt.Sprintf(summaryOmittedMessage, len(span))}
	avail := budget - summaryInputTokens([]provider.Message{marker})
	var head, tail []provider.Message
	acc, i, j := 0, 0, len(span)-1
	for i <= j {
		fromHead := len(head) <= len(tail)*2
		idx := j
		if fromHead {
			idx = i
		}
		cost := summaryInputTokens(span[idx : idx+1])
		if acc+cost > avail {
			break
		}
		acc += cost
		if fromHead {
			head = append(head, span[i])
			i++
			continue
		}
		tail = append([]provider.Message{span[j]}, tail...)
		j--
	}
	dropped := len(span) - len(head) - len(tail)
	if dropped <= 0 {
		return span
	}
	if len(head)+len(tail) == 0 {
		return []provider.Message{truncateMessageToBudget(span[0], budget)}
	}
	marker.Content = fmt.Sprintf(summaryOmittedMessage, dropped)
	out := make([]provider.Message, 0, len(head)+len(tail)+1)
	out = append(out, head...)
	out = append(out, marker)
	return append(out, tail...)
}

// truncateMessageToBudget shortens one message that alone exceeds a call,
// keeping its head and tail so the digest still sees how it started and ended.
func truncateMessageToBudget(m provider.Message, budget int) provider.Message {
	full := []rune(m.Content)
	framing := m
	framing.Content = ""
	// The marker and the transcript framing both count against the budget, and
	// the marker widens with the count it reports, so size both up front.
	keep := budget - len([]rune(fmt.Sprintf(summaryTruncateMarker, len(full)))) - summaryInputTokens([]provider.Message{framing})
	if keep <= 0 || len(full) <= keep {
		return m
	}
	head := keep * 2 / 3
	m.Content = string(full[:head]) + fmt.Sprintf(summaryTruncateMarker, len(full)-keep) + string(full[len(full)-(keep-head):])
	return m
}

func joinSummaryInstructions(base, extra string) string {
	base, extra = strings.TrimSpace(base), strings.TrimSpace(extra)
	if base == "" {
		return extra
	}
	if extra == "" {
		return base
	}
	return base + "\n\n" + extra
}

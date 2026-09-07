package agent

import (
	"context"
	"strings"

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
	foldTokens := summaryInputTokens(fold)
	budget := a.summaryInputBudget(instructions)

	if budget > 0 && foldTokens > budget {
		// Fold exceeds budget — use chunked summarization.
		return a.chunkedFoldToSummary(ctx, fold, instructions, inputMode, budget, foldTokens)
	}

	res := foldSummary{Mode: CompactionModeSummarized, Spans: 1, FoldTokens: foldTokens, InputMode: inputMode}
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

// chunkedFoldToSummary splits an oversized fold into overlapping chunks from
// tail to head with exponentially growing budgets, summarizes each chunk
// independently, and merges the results. This resolves the compression deadlock
// for sessions that have grown beyond the context window.
func (a *Agent) chunkedFoldToSummary(ctx context.Context, fold []provider.Message, instructions, inputMode string, budget, foldTokens int) (foldSummary, error) {
	chunks := splitExponentialChunks(fold, budget, 16*1024)

	var summaries []string
	var totalUsage *provider.Usage
	var lastReqID string
	for _, chunk := range chunks {
		summary, _, usage, reqID, err := a.runCompactionSummary(ctx, chunk, instructions)
		if err != nil {
			return foldSummary{FoldTokens: foldTokens, Spans: len(chunks), InputMode: inputMode}, err
		}
		summaries = append(summaries, summary)
		totalUsage = mergeCompactionUsage(totalUsage, usage)
		if reqID != "" {
			lastReqID = reqID
		}
	}

	return foldSummary{
		Text:       strings.Join(summaries, "\n\n"),
		Mode:       CompactionModeSummarized,
		FoldTokens: foldTokens,
		Spans:      len(chunks),
		Usage:      totalUsage,
		RequestID:  lastReqID,
		InputMode:  inputMode,
	}, nil
}

// splitExponentialChunks splits fold messages from tail to head into chunks
// whose token budgets grow exponentially: baseBudget, 2×, 4×, 8×, capped at
// 768k tokens. Each chunk overlaps the next by overlapTokens to preserve
// boundary continuity. Chunk boundaries never split a single message.
func splitExponentialChunks(fold []provider.Message, baseBudget, overlapTokens int) [][]provider.Message {
	if baseBudget <= 0 || len(fold) == 0 {
		return [][]provider.Message{fold}
	}
	const maxCap = 768 * 1024

	var chunks [][]provider.Message
	end := len(fold)
	chunkBudget := baseBudget

	for end > 0 {
		if chunkBudget > maxCap {
			chunkBudget = maxCap
		}
		start := end
		acc := 0
		for i := end - 1; i >= 0; i-- {
			tok := estimateMessagesTokens(fold[i : i+1])
			if acc+tok > chunkBudget && start < end {
				break
			}
			acc += tok
			start = i
		}
		if start >= end {
			start = end - 1
		}
		chunks = append(chunks, fold[start:end])

		nextEnd := start
		if nextEnd > 0 {
			overlapAcc := 0
			for i := start - 1; i >= 0; i-- {
				tok := estimateMessagesTokens(fold[i : i+1])
				if overlapAcc+tok > overlapTokens {
					break
				}
				overlapAcc += tok
				nextEnd = i
			}
		}
		end = nextEnd
		chunkBudget *= 2
	}

	// Reverse so chunks go from oldest to newest (head to tail).
	for i, j := 0, len(chunks)-1; i < j; i, j = i+1, j-1 {
		chunks[i], chunks[j] = chunks[j], chunks[i]
	}
	return chunks
}

func mergeCompactionUsage(a, b *provider.Usage) *provider.Usage {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	return &provider.Usage{
		PromptTokens:     a.PromptTokens + b.PromptTokens,
		CompletionTokens: a.CompletionTokens + b.CompletionTokens,
		TotalTokens:      a.TotalTokens + b.TotalTokens,
		CacheReadTokens:  a.CacheReadTokens + b.CacheReadTokens,
		CacheWriteTokens: a.CacheWriteTokens + b.CacheWriteTokens,
	}
}

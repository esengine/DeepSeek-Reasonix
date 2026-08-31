package agent

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"sync"

	"reasonix/internal/provider"
)

// Chunked session recovery summarizes an over-length transcript in
// exponentially decaying fragments and tree-reduces their digests.
// It powers the #9082 in-place compaction fallback.

const (
	// Chunk byte budgets from newest to oldest (design doc 2026-08-18:
	// 会话引用与压缩机制分析). Chunks older than the listed sizes stay at
	// the last size - coarse folding is fine for ancient history.
	extractChunkNewestBytes = 64 << 10
	extractChunkNextBytes   = 128 << 10
	extractChunkThirdBytes  = 256 << 10
	extractChunkFourthBytes = 512 << 10
	extractChunkOldestBytes = 768 << 10
	// Adjacent chunks share this many bytes near their boundary so a fact
	// spanning the cut is not lost between digests.
	extractChunkOverlapBytes = 16 << 10
	minMergeInputTokens      = 400
	// A chunked recovery owns the compaction lock and can issue multiple paid
	// requests. Keep the exceptional path finite even when a provider repeatedly
	// truncates a digest without making it smaller.
	maxChunkedSummaryCalls = 64
	maxChunkedMergeDepth   = 8
)

const extractFragmentInstructionTmpl = `This is fragment %d/%d of an over-length session being recovered after its context exceeded the model window. Compact the preceding fragment into a durable briefing under these exact headings, omitting a heading only if it has no content:

## Standing facts & constraints
## Goal
## Decisions & rationale
## Files & code
## Commands & outcomes
## Errors & fixes
## Pending & next step

Rules: be terse - bullet points and fragments, not prose. Preserve identifiers, paths, and numbers exactly. This fragment sits earlier in the session than newer ones will, so capture everything this fragment establishes even if it may be refined later. Do NOT invent anything not present in the messages; if something is unknown, leave it out rather than guessing. Output only the structured Markdown briefing. Do not call tools. Do not output reasoning.`

const extractMergeInstruction = `The following are sequential fragment briefings of one over-length session, ordered oldest to newest. Merge them into the session's final resume briefing under the same exact headings (## Standing facts & constraints / ## Goal / ## Decisions & rationale / ## Files & code / ## Commands & outcomes / ## Errors & fixes / ## Pending & next step). Later fragments supersede earlier ones - keep the final state of every fact, drop superseded entries, and preserve identifiers, paths, and numbers exactly. Output only the structured Markdown briefing. Do not call tools. Do not output reasoning.`

type extractMessageSpan struct{ lo, hi int }

// extractMessageUnits returns replay-safe units. An assistant tool-call message
// and all of its contiguous results are indivisible because every provider
// adapter sanitizes that pairing immediately before serialization.
func extractMessageUnits(msgs []provider.Message) []extractMessageSpan {
	units := make([]extractMessageSpan, 0, len(msgs))
	for i := 0; i < len(msgs); {
		j := i + 1
		switch {
		case msgs[i].Role == provider.RoleAssistant && len(msgs[i].ToolCalls) > 0:
			for j < len(msgs) && msgs[j].Role == provider.RoleTool {
				j++
			}
		case msgs[i].Role == provider.RoleTool:
			// Keep malformed/orphan result runs together too. Sanitization may drop
			// them, but chunking must never create additional orphan boundaries.
			for j < len(msgs) && msgs[j].Role == provider.RoleTool {
				j++
			}
		}
		units = append(units, extractMessageSpan{lo: i, hi: j})
		i = j
	}
	return units
}

func extractUnitWireBytes(msgs []provider.Message, unit extractMessageSpan, policy provider.SharedWindowInputPolicy) int {
	total := 0
	for _, msg := range msgs[unit.lo:unit.hi] {
		total += messageWireBytes(msg, policy)
	}
	return total
}

func extractInstructionWithFocus(base, instructions string) string {
	if strings.TrimSpace(instructions) == "" {
		return base
	}
	return base + "\n\nAdditional focus for this compaction (prioritize keeping this):\n" + strings.TrimSpace(instructions)
}

func extractFragmentInstruction(index, total int, instructions string) string {
	return extractInstructionWithFocus(fmt.Sprintf(extractFragmentInstructionTmpl, index, total), instructions)
}

func extractMergeInstructionWithFocus(instructions string) string {
	return extractInstructionWithFocus(extractMergeInstruction, instructions)
}

type chunkedSummaryRun struct {
	a     *Agent
	calls int
	usage *provider.Usage
}

func newChunkedSummaryRun(a *Agent) *chunkedSummaryRun {
	return &chunkedSummaryRun{a: a}
}

func (r *chunkedSummaryRun) requireCalls(required int) error {
	if required < 0 {
		return fmt.Errorf("invalid chunked summary call reservation (%d)", required)
	}
	if r.calls+required > maxChunkedSummaryCalls {
		return fmt.Errorf("chunked summary call budget exhausted (%d): %d used, %d required", maxChunkedSummaryCalls, r.calls, required)
	}
	return nil
}

func (r *chunkedSummaryRun) summarize(ctx context.Context, fold []provider.Message, instructions string, reserveAfter int) (foldSummary, error) {
	if err := ctx.Err(); err != nil {
		return foldSummary{}, err
	}
	if err := r.requireCalls(1 + reserveAfter); err != nil {
		return foldSummary{}, err
	}
	r.calls++
	res, err := r.a.foldToSummary(ctx, fold, instructions)
	r.usage = mergeSamplingUsage(r.usage, res.Usage)
	return res, err
}

// extractChunkSizes returns the per-chunk byte budgets from newest to oldest.
func extractChunkSizes() []int {
	return []int{
		extractChunkNewestBytes,
		extractChunkNextBytes,
		extractChunkThirdBytes,
		extractChunkFourthBytes,
		extractChunkOldestBytes,
	}
}

func minimumChunkedSummaryCalls(chunkCount int) int {
	if chunkCount <= 0 {
		return 0
	}
	if chunkCount == 1 {
		return 1
	}
	return chunkCount + 1
}

// messageWireBytes approximates the provider-visible transcript footprint of
// one message. chunkedFoldSummary first removes local-only/raw fields through
// modelInputMessages, so local storage metadata cannot distort boundaries.
func messageWireBytes(msg provider.Message, policy provider.SharedWindowInputPolicy) int {
	chars, _, _ := requestCalibrationTextShape(provider.Request{Messages: []provider.Message{msg}}, policy)
	n := int(chars) + len(msg.ReasoningID) + len(msg.ReasoningStatus) + len(msg.ReasoningSignature)
	for _, tc := range msg.ToolCalls {
		n += len(tc.ThoughtSignature)
	}
	for _, img := range msg.Images {
		n += len(img)
	}
	return n
}

// splitExtractChunks splits messages newest-tail-first into chunks following
// the exponential size table, with adjacent chunks sharing `overlap` bytes
// around their boundary. Boundaries never split a replay-safe message unit:
// tool calls and their contiguous results stay together even when that
// overshoots the budget. Returns
// chunks oldest-first. A transcript smaller than the newest chunk yields a
// single chunk covering everything.
func splitExtractChunks(msgs []provider.Message, overlap int, policy provider.SharedWindowInputPolicy) [][]provider.Message {
	if len(msgs) == 0 {
		return nil
	}
	units := extractMessageUnits(msgs)
	sizes := extractChunkSizes()
	var spans []extractMessageSpan // unit indexes, newest -> oldest
	end := len(units)
	for i := 0; end > 0; i++ {
		size := sizes[min(i, len(sizes)-1)]
		if i > 0 {
			size -= overlap // the shared boundary region is counted by the newer chunk
		}
		lo := end
		acc := 0
		for lo > 0 && acc < size {
			lo--
			acc += extractUnitWireBytes(msgs, units[lo], policy)
		}
		spans = append(spans, extractMessageSpan{lo: lo, hi: end})
		end = lo
	}
	// Widen every older chunk's right edge into its newer neighbor's head so
	// the shared overlap bytes are summarized twice, keeping boundary facts
	// in both digests.
	for j := 1; j < len(spans); j++ {
		hi := spans[j].hi // the older chunk ends where the newer one begins
		acc := 0
		for hi < spans[j-1].hi && acc < overlap {
			acc += extractUnitWireBytes(msgs, units[hi], policy)
			hi++
		}
		spans[j].hi = hi
	}
	chunks := make([][]provider.Message, 0, len(spans))
	for _, current := range slices.Backward(spans) { // oldest first
		lo := units[current.lo].lo
		hi := units[current.hi-1].hi
		chunks = append(chunks, msgs[lo:hi])
	}
	return chunks
}

// chunkedFoldSummary summarizes a fold too large for one summarizer request:
// byte-budgeted fragments (exponentially decaying, newest finest), each
// summarized with the resilient half-split retry, and the digests merged via
// tree-reduce. It is the compaction fallback for over-length folds (#9082
// #9572 follow-up): the projection still installs in the same session, so
// work continues in place. progress, when non-nil, reports (chunks
// summarized, total chunks); the total grows when a fragment splits.
func (a *Agent) chunkedFoldSummary(ctx context.Context, fold []provider.Message, instructions string, progress func(done, total int)) (result foldSummary, err error) {
	fold = modelInputMessages(fold)
	result = foldSummary{
		Mode:       CompactionModeChunked,
		FoldTokens: summaryInputTokens(fold),
		InputMode:  SummaryInputChunked,
	}
	if len(fold) == 0 {
		return result, fmt.Errorf("fold is empty")
	}
	run := newChunkedSummaryRun(a)
	defer func() {
		result.Usage = run.usage
		result.Spans = run.calls
	}()
	chunks := splitExtractChunks(fold, extractChunkOverlapBytes, sharedWindowInputPolicyOf(a.svc.prov))
	if len(chunks) == 0 {
		return result, fmt.Errorf("fold is empty")
	}
	text, err := a.summarizeExtractChunks(ctx, chunks, instructions, progress, run)
	if err != nil {
		return result, err
	}
	result.Text = text
	return result, nil
}

func (a *Agent) summarizeExtractChunks(ctx context.Context, chunks [][]provider.Message, instructions string, progress func(done, total int), run *chunkedSummaryRun) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if len(chunks) == 0 {
		return "", fmt.Errorf("no extract chunks to summarize")
	}
	if err := run.requireCalls(minimumChunkedSummaryCalls(len(chunks))); err != nil {
		return "", err
	}
	report := orNoopProgress(progress)
	// Fragments may split in half on summarizer failure (see
	// extractFragmentResilient), so the progress total grows as splits happen.
	var progressMu sync.Mutex
	done, total := 0, len(chunks)
	advance := func(grown bool) {
		progressMu.Lock()
		defer progressMu.Unlock()
		if grown {
			total++
		} else {
			done++
		}
		report(done, total)
	}
	parts := make([]string, 0, len(chunks))
	mergeInstructions := extractMergeInstructionWithFocus(instructions)
	for i, chunk := range chunks {
		fragInstructions := extractFragmentInstruction(i+1, len(chunks), instructions)
		reserveAfter := len(chunks) - i - 1
		if len(chunks) > 1 {
			reserveAfter++
		}
		res, err := a.extractFragmentResilient(ctx, chunk, fragInstructions, mergeInstructions, advance, run, reserveAfter)
		if err != nil {
			return "", fmt.Errorf("fragment %d/%d: %w", i+1, len(chunks), err)
		}
		parts = append(parts, res)
	}
	text, err := a.mergeFragmentsWithRun(ctx, parts, mergeInstructions, run, 0)
	if err != nil {
		return "", err
	}
	return text, nil
}

// extractFragmentResilient summarizes one extract fragment, splitting it in
// half and extracting each half when the fragment cannot be summarized whole:
// a very large fragment makes the summarizer output run into the provider's
// output-token limit (the same failure that blocks in-place compaction on
// over-length sessions), and on small-window models the fragment itself can
// overflow the input window. Both are fixed by smaller fragments, so the
// halves are extracted and their digests merged. Replay-safe units and the
// shared call budget bound the recovery. report(true) grows the progress total
// (one fragment became two); report(false) marks one leaf fragment summarized.
func (a *Agent) extractFragmentResilient(ctx context.Context, chunk []provider.Message, instructions, mergeInstructions string, report func(grown bool), run *chunkedSummaryRun, reserveAfter int) (string, error) {
	res, err := run.summarize(ctx, chunk, instructions, reserveAfter)
	if err == nil {
		return strings.TrimSpace(res.Text), nil
	}
	retriable := errors.Is(err, errSummaryOutputTruncated) || errors.Is(err, ErrCompactionRequired)
	leftChunk, rightChunk, splittable := splitExtractFragment(chunk)
	if !retriable || !splittable {
		return "", err
	}
	report(true)
	left, err := a.extractFragmentResilient(ctx, leftChunk, instructions, mergeInstructions, report, run, reserveAfter+2)
	if err != nil {
		return "", err
	}
	right, err := a.extractFragmentResilient(ctx, rightChunk, instructions, mergeInstructions, report, run, reserveAfter+1)
	if err != nil {
		return "", err
	}
	merged, err := a.mergeFragmentsWithRun(ctx, []string{left, right}, mergeInstructions, run, reserveAfter)
	if err != nil {
		return "", fmt.Errorf("merge split fragments: %w", err)
	}
	return merged, nil
}

func splitExtractFragment(chunk []provider.Message) (left, right []provider.Message, ok bool) {
	units := extractMessageUnits(chunk)
	if len(units) < 2 {
		return nil, nil, false
	}
	boundary := units[len(units)/2].lo
	return chunk[:boundary], chunk[boundary:], true
}

// mergeInputBudget is the merge-request input ceiling in tokens: half of the
// safe summarizer input, leaving room for the merge instruction and the
// digest output inside the same request. An unknown window disables the
// pre-splitting (the request fails into the provider's own error then).
func (a *Agent) mergeInputBudget() int {
	window := a.effectiveContextWindow()
	if window <= 0 {
		return math.MaxInt
	}
	return max(minMergeInputTokens, (window-a.summaryOutputBudget()-protocolReserveTokens)/2)
}

// mergeGroup merges one group of fragment briefings. A group that cannot be
// summarized whole (output truncation, input overflow) splits in half and
// recurses — the briefings are already in hand, so the merge must not fail
// with them discarded (#9082 follow-up).
func (a *Agent) mergeGroup(ctx context.Context, group []string, instructions string, run *chunkedSummaryRun, depth, reserveAfter int) (string, error) {
	merged, err := run.summarize(ctx, mergeDigestMessages(group), instructions, reserveAfter)
	if err == nil {
		return strings.TrimSpace(merged.Text), nil
	}
	mergeErr := err
	retriable := errors.Is(err, errSummaryOutputTruncated) || errors.Is(err, ErrCompactionRequired)
	if !retriable || len(group) < 2 {
		return "", err
	}
	if depth >= maxChunkedMergeDepth {
		return "", fmt.Errorf("merge recovery depth exhausted (%d): %w", maxChunkedMergeDepth, err)
	}
	before := estimateMessagesTokens(mergeDigestMessages(group))
	mid := len(group) / 2
	left, err := a.mergeGroup(ctx, group[:mid], instructions, run, depth+1, reserveAfter+2)
	if err != nil {
		return "", err
	}
	right, err := a.mergeGroup(ctx, group[mid:], instructions, run, depth+1, reserveAfter+1)
	if err != nil {
		return "", err
	}
	next := []string{left, right}
	after := estimateMessagesTokens(mergeDigestMessages(next))
	if after >= before {
		return "", fmt.Errorf("merge recovery made no progress (%d >= %d tokens): %w", after, before, mergeErr)
	}
	return a.mergeGroup(ctx, next, instructions, run, depth+1, reserveAfter)
}

// mergeFragments merges fragment briefings into one final briefing. When the
// whole set would overflow the merge request (many fragments, or a small
// provider window), it is merged pairwise first — tree-reduce, so the merge
// never fails with every fragment briefing already in hand.
func (a *Agent) mergeFragments(ctx context.Context, parts []string) (string, error) {
	run := newChunkedSummaryRun(a)
	return a.mergeFragmentsWithRun(ctx, parts, extractMergeInstruction, run, 0)
}

func (a *Agent) mergeFragmentsWithRun(ctx context.Context, parts []string, instructions string, run *chunkedSummaryRun, reserveAfter int) (string, error) {
	if len(parts) == 0 {
		return "", fmt.Errorf("no fragment briefings to merge")
	}
	parts = append([]string(nil), parts...)
	for len(parts) > 1 && estimateMessagesTokens(mergeDigestMessages(parts)) > a.mergeInputBudget() {
		pairCalls := len(parts) / 2
		futureMerge := 0
		if (len(parts)+1)/2 > 1 {
			futureMerge = 1
		}
		if err := run.requireCalls(pairCalls + futureMerge + reserveAfter); err != nil {
			return "", err
		}
		var next []string
		pairIndex := 0
		for i := 0; i < len(parts); i += 2 {
			group := parts[i:min(i+2, len(parts))]
			if len(group) == 1 {
				// Odd tail: carried into the next round unchanged.
				next = append(next, group[0])
				continue
			}
			pairIndex++
			pairReserve := pairCalls - pairIndex + futureMerge + reserveAfter
			merged, err := a.mergeGroup(ctx, group, instructions, run, 0, pairReserve)
			if err != nil {
				return "", err
			}
			next = append(next, merged)
		}
		if len(next) >= len(parts) {
			break // defensive: cannot shrink further; try the plain merge
		}
		parts = next
	}
	if len(parts) == 1 {
		return parts[0], nil
	}
	merged, err := a.mergeGroup(ctx, parts, instructions, run, 0, reserveAfter)
	if err != nil {
		return "", fmt.Errorf("merge: %w", err)
	}
	return merged, nil
}

// mergeDigestMessages builds the merge request body: one user message per
// fragment briefing, oldest first, so the summarizer sees the timeline.
func mergeDigestMessages(parts []string) []provider.Message {
	msgs := make([]provider.Message, 0, len(parts)+1)
	msgs = append(msgs, provider.Message{
		Role:    provider.RoleUser,
		Content: fmt.Sprintf("Session fragment briefings (%d fragments, oldest to newest):", len(parts)),
	})
	for i, part := range parts {
		msgs = append(msgs, provider.Message{
			Role:    provider.RoleUser,
			Content: fmt.Sprintf("<fragment index=%d>\n%s\n</fragment>", i+1, part),
		})
	}
	return msgs
}

func orNoopProgress(progress func(done, total int)) func(done, total int) {
	if progress != nil {
		return progress
	}
	return func(done, total int) {}
}

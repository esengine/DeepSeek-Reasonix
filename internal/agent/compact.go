package agent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"reasonix/internal/ablation"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// Compaction is a low-frequency cache-reset point: the prompt grows append-only
// (high cache hits) until a turn nears compactRatio of the window, then it is
// compacted down to a tail budget. The budget is a fixed token count, not a
// fraction of the window, so a huge window still compacts rarely while a small
// one still lands below the trigger (which is what stops the re-compaction loop).
const (
	defaultSoftCompactRatio    = 0.5   // report growing context here, but keep the cache-stable prefix intact
	defaultToolResultSnipRatio = 0.6   // rewrite stale tool results cheaply before summary compaction
	defaultCompactRatio        = 0.8   // trigger: prompt at this fraction of the window compacts
	defaultCompactForceRatio   = 0.9   // force compaction at this high-water mark even for low-value folds
	defaultCompactTarget       = 0.5   // safety cap: the kept tail never exceeds this fraction of the window
	defaultTailTokens          = 16384 // verbatim recent-tail budget, in tokens
	minRecentKeep              = 2     // never keep fewer recent messages than this
	minCompactMessages         = 2     // skip compaction below this many compactable messages
	fallbackTokPerChar         = 0.25  // ~4 chars/token, used before any usage is available to calibrate
	maxPinnedFirstUserTokens   = 1500  // ceiling on pinning the first user turn verbatim; larger first turns (pasted content) stay foldable
	pinnedFirstUserWindowFrac  = 0.15  // and never pin a first turn worth more than this fraction of the window
	maxCarriedDigestTokens     = 12000 // ceiling on digests carried verbatim across folds before one consolidating fold merges them
	maxEarlyUserTurns          = 3     // small user turns hoisted verbatim ahead of the digest; position-fixed (the first N of the fold region, never "the latest N") so the projection prefix stays byte-stable
	protocolReserveTokens      = 256   // provider framing and control fields not represented by message estimates
)

var errSummaryOutputTruncated = errors.New("summarizer output truncated")

// summaryTag wraps the compaction summary so the model can distinguish it from
// live user input and later strip or skip it when reasoning about the current turn.
const (
	summaryTagOpen  = "<compaction-summary>"
	summaryTagClose = "</compaction-summary>"
)

// summaryTimeout bounds one summarizer call so a stalled stream surfaces a clear
// failure (then a mechanical fold) instead of hanging compaction indefinitely.
const summaryTimeout = 90 * time.Second

// summarySystemPrompt steers the executor to distill older history into a
// structured briefing it can keep relying on after the originals are dropped.
// The section layout mirrors what a coding agent actually needs to resume work
// mid-task: the goal verbatim, the concrete state of the code, and an explicit
// next step — so the post-compaction turn doesn't lose the thread or re-derive
// decisions already made.
const summarySystemPrompt = `You are compacting the earlier part of a coding agent's conversation to save context.
The agent keeps your summary alongside the user's own turns (kept verbatim) and the recent tail; your job is to fold the assistant/tool work into a briefing it can resume from.
Write under these exact headings, omitting a heading only if it has no content:

## Standing facts & constraints
Everything the user stated that still governs the work — names, paths, IDs, versions, tokens, preferences, and hard "never do X" rules — in their own words. Be exhaustive; this is the durable contract, so prefer over- to under-including.

## Goal
The user's request and intent.

## Decisions & rationale
Key choices made so far and why — so they are not re-litigated or reversed.

## Files & code
Files read or modified, with the specific facts that matter: signatures, line locations, data shapes, and exact edits applied. Be concrete; this is what lets the agent act without re-reading everything.

## Commands & outcomes
Commands run (builds, tests, git) and their relevant results — what passed, what failed, and the error text that matters.

## Errors & fixes
Problems hit and how they were resolved (or not), so the same dead ends are not repeated.

## Pending & next step
What is still in progress or unstarted, and the single most concrete next action to take.

Rules: be terse — bullet points and fragments, not prose. Preserve identifiers, paths, and numbers exactly. Do NOT invent anything not present in the messages; if something is unknown, leave it out rather than guessing.`

// compactThresholds returns the prompt-token boundaries ContextManager switches
// on. The compaction ablation arm collapses the snip and fold triggers onto
// soft, so the cache-preserving deferral branch is unreachable and the session
// folds as soon as it grows — what a harness with no prompt-cache strategy does.
func (a *Agent) compactThresholds() (soft, snip, high int) {
	hard := a.hardInputCeiling()
	high = int(float64(a.contextWindow) * a.compactRatio)
	// Ratios >= 1 are the documented test/config escape hatch for disabling
	// automatic compaction; do not turn that into an immediate hard-trigger by
	// clamping it down to the output-budget ceiling.
	if a.compactRatio < 1 {
		high = minInt(high, hard)
	}
	snip = minInt(int(float64(a.contextWindow)*a.toolResultSnipRatio), high)
	soft = minInt(int(float64(a.contextWindow)*a.softCompactRatio), high)
	if a.ablation.Off(ablation.Compaction) {
		high, snip = soft, soft
	}
	return soft, snip, high
}

// hardInputCeiling leaves room for the configured output and protocol framing.
// A zero output budget means the provider's input window is the only declared
// limit, preserving the historical behavior for providers that manage output
// tokens themselves.
func (a *Agent) hardInputCeiling() int {
	if a == nil || a.contextWindow <= 0 {
		return 0
	}
	hard := a.contextWindow
	outputBudget := a.maxOutputTokens
	if outputBudget <= 0 && sharesContextWindow(a.prov) {
		outputBudget = a.outputBudget
	}
	if outputBudget > 0 {
		hard -= outputBudget + protocolReserveTokens
	}
	return max(1, hard)
}

func (a *Agent) forceCompactThreshold(high int) int {
	hard := a.hardInputCeiling()
	force := minInt(int(float64(a.contextWindow)*a.compactForceRatio), hard)
	return max(high, force)
}

// minimumMaintenanceSavingsTokens avoids cache-breaking projections that save
// too little to matter. Small synthetic windows can never reach the production
// floor; those are allowed through when they are already at the hard ceiling.
func (a *Agent) minimumMaintenanceSavingsTokens() int {
	if a == nil || a.contextWindow <= 0 {
		return 0
	}
	n := int(float64(a.contextWindow) * 0.02)
	return minInt(max(n, 4096), 20000)
}

// foldEconomics estimates whether compacting the given region saves enough
// tokens to justify the summarization API call. It returns false when the
// region is too small for the savings to outweigh the extra round-trip cost
// and latency of calling the summarizer.
func foldEconomics(region []provider.Message) bool {
	const minFoldTokens = 400
	return estimateMessagesTokens(region) >= minFoldTokens
}

func estimateMessagesTokens(msgs []provider.Message) int {
	total := 0
	for _, m := range msgs {
		if m.LocalOnly {
			continue
		}
		total += 4 // chat-message framing overhead
		total += estimateTextTokens(m.Content)
		total += estimateTextTokens(m.ReasoningContent)
		total += estimateTextTokens(m.Name)
		total += estimateTextTokens(m.ToolCallID)
		for _, tc := range m.ToolCalls {
			total += 8
			total += estimateTextTokens(tc.ID)
			total += estimateTextTokens(tc.Name)
			total += estimateTextTokens(tc.Arguments)
		}
		for _, item := range m.ResponsesItems {
			total += estimateTextTokens(string(item))
		}
	}
	return total
}

func estimateTextTokens(s string) int {
	if s == "" {
		return 0
	}
	// A conservative cross-language approximation: English-ish text trends near
	// four bytes per token, while CJK-heavy text is closer to one rune per token.
	bytes := len(s)
	runes := utf8.RuneCountInString(s)
	byBytes := (bytes + 3) / 4
	if runes > byBytes {
		return runes
	}
	return byBytes
}

// SummarizeFrom keeps the compatibility index contract while installing a
// projection that compresses from that user-turn boundary onward.
func (a *Agent) SummarizeFrom(ctx context.Context, fromIdx int) error {
	return a.summarizeAtProjectionBoundary(ctx, fromIdx, "after")
}

// SummarizeUpTo keeps the compatibility index contract while installing a
// projection that compresses everything before that user-turn boundary.
func (a *Agent) SummarizeUpTo(ctx context.Context, toIdx int) error {
	return a.summarizeAtProjectionBoundary(ctx, toIdx, "before")
}

func (a *Agent) summarizeAtProjectionBoundary(ctx context.Context, canonicalIndex int, direction string) error {
	snap := a.snapshotExplicitCompression()
	if canonicalIndex < 0 || canonicalIndex >= len(snap.canonical) {
		return nil
	}
	anchor := snap.canonical[canonicalIndex]
	if !compressAnchorCandidate(anchor) {
		return nil
	}
	visibleIndex := -1
	for i, msg := range snap.visible {
		if !compressAnchorCandidate(msg) {
			continue
		}
		if anchor.CreatedAt != 0 && msg.CreatedAt == anchor.CreatedAt {
			visibleIndex = i
			break
		}
		if anchor.CreatedAt == 0 && UserMessageText(msg) == UserMessageText(anchor) {
			if visibleIndex >= 0 {
				return fmt.Errorf("summarize boundary is ambiguous in the current model context")
			}
			visibleIndex = i
		}
	}
	if visibleIndex < 0 {
		return fmt.Errorf("context compression unavailable: selected turn is no longer present in the model context")
	}
	result, err := a.compressVisibleRange(ctx, snap, CompactionTriggerManual, direction, visibleIndex, anchorPreview(UserMessageText(anchor)), "")
	if err != nil {
		return err
	}
	if result.Status != "ok" {
		reason := strings.TrimSpace(result.Reason)
		if reason == "" {
			reason = "selected range did not reduce the model context"
		}
		return fmt.Errorf("context compression skipped: %s", reason)
	}
	return nil
}

// IsCompactionSummary reports whether m is a rolling digest inserted by a
// prior compaction fold. Exported for session owners outside this package
// (e.g. the guardian) whose turn rollback must not treat a digest as a
// disposable user message.
func IsCompactionSummary(m provider.Message) bool { return isCompactionSummary(m) }

func (a *Agent) activeTurnStart(msgs []provider.Message) int {
	createdAt := a.activeTurnCreatedAt.Load()
	if createdAt == 0 {
		return -1
	}
	for i, m := range msgs {
		if m.Role == provider.RoleUser && m.CreatedAt == createdAt {
			return i
		}
	}
	return -1
}

// isCompactionSummary reports whether m is a rolling summary from a prior fold.
func isCompactionSummary(m provider.Message) bool {
	return m.Role == provider.RoleUser &&
		strings.HasPrefix(strings.TrimLeft(m.Content, "\n "), summaryTagOpen)
}

// pinnedPrefixLen counts the leading messages a fold keeps verbatim ahead of
// everything else: the system prompt and the first user turn (its task + stated
// facts/constraints) when it is small enough to be a brief. Digests are never
// pinned — any digest in the transcript enters the fold region and is merged
// into the next one, so a session cannot accumulate a chain of them.
func (a *Agent) pinnedPrefixLen(msgs []provider.Message) int {
	i := 0
	if i < len(msgs) && msgs[i].Role == provider.RoleSystem {
		i++
	}
	if i < len(msgs) && msgs[i].Role == provider.RoleUser && !isCompactionSummary(msgs[i]) && a.fixedPinnableUserTurn(msgs[i]) {
		i++
	}
	return i
}

// fixedPinnableUserTurn reports whether a user turn is small enough to keep
// verbatim in a position-stable prefix. Identity decisions must not use the
// latest provider usage: after projection activates, that usage describes the
// projection while the canonical transcript remains larger, which would make
// the same turn drift in or out across compactions. Dynamic token calibration is
// reserved for non-identity estimates such as tail sizing.
func (a *Agent) fixedPinnableUserTurn(m provider.Message) bool {
	budget := maxPinnedFirstUserTokens
	if a.contextWindow > 0 {
		if f := int(float64(a.contextWindow) * pinnedFirstUserWindowFrac); f < budget {
			budget = f
		}
	}
	return int(float64(msgChars(m))*fallbackTokPerChar) <= budget
}

func keepIndexes(region []provider.Message, policy KeepPolicy) []bool {
	keep := make([]bool, len(region))
	policyStart := 0
	for i, m := range region {
		if isCompactionSummary(m) {
			policyStart = i + 1
		}
	}
	// Retention applies only to messages since the latest digest; older kept
	// messages are allowed to fold on the next pass so they cannot grow forever.
	for i, m := range region {
		if i >= policyStart && shouldKeepMessage(m, policy) {
			keep[i] = true
		}
	}
	for i, m := range region {
		if !keep[i] {
			continue
		}
		switch m.Role {
		case provider.RoleTool:
			if j := findToolCaller(region, i, m.ToolCallID); j >= 0 {
				keepToolCallGroup(region, keep, j)
			}
		case provider.RoleAssistant:
			keepToolCallGroup(region, keep, i)
		}
	}
	return keep
}

func keepToolCallGroup(region []provider.Message, keep []bool, assistantIndex int) {
	if assistantIndex < 0 || assistantIndex >= len(region) {
		return
	}
	m := region[assistantIndex]
	if m.Role != provider.RoleAssistant || len(m.ToolCalls) == 0 {
		return
	}
	keep[assistantIndex] = true
	ids := toolCallIDs(m)
	for j := assistantIndex + 1; j < len(region) && region[j].Role == provider.RoleTool; j++ {
		if ids[region[j].ToolCallID] {
			keep[j] = true
		}
	}
}

func shouldKeepMessage(m provider.Message, policy KeepPolicy) bool {
	if policy&KeepErrors != 0 && isErrorMessage(m) {
		return true
	}
	if policy&KeepUserMarked != 0 && isUserMarked(m) {
		return true
	}
	return false
}

func isErrorMessage(m provider.Message) bool {
	if m.Role != provider.RoleTool {
		return false
	}
	if failedExecution(m.ToolExecution) {
		return true
	}
	s := strings.TrimSpace(strings.ToLower(m.Content))
	return strings.HasPrefix(s, "error:") || strings.HasPrefix(s, "blocked:")
}

// failedExecution reads the failure the host already recorded, rather than
// guessing from the text. A `go test` run that reports FAIL exits non-zero
// while its output starts with "=== RUN", which no prefix match can see.
func failedExecution(ex *provider.ToolExecution) bool {
	if ex == nil {
		return false
	}
	if ex.State == tool.ShellStateFailed || ex.State == tool.ShellStateTimedOut {
		return true
	}
	if ex.ExitCode != nil && *ex.ExitCode != 0 {
		return true
	}
	return ex.Verification == tool.ShellVerificationFailed
}

func isUserMarked(m provider.Message) bool {
	if m.Role != provider.RoleUser {
		return false
	}
	content := strings.TrimSpace(strings.ToLower(m.Content))
	return strings.HasPrefix(content, "[[keep]]") ||
		strings.HasPrefix(content, "[keep]") ||
		strings.HasPrefix(content, "<keep>") ||
		strings.HasPrefix(content, "<!-- keep -->")
}

func findToolCaller(region []provider.Message, toolIndex int, id string) int {
	for i := toolIndex - 1; i >= 0; i-- {
		if region[i].Role != provider.RoleAssistant {
			continue
		}
		for _, tc := range region[i].ToolCalls {
			if tc.ID == id {
				return i
			}
		}
	}
	return -1
}

func toolCallIDs(m provider.Message) map[string]bool {
	ids := make(map[string]bool, len(m.ToolCalls))
	for _, tc := range m.ToolCalls {
		ids[tc.ID] = true
	}
	return ids
}

// planCompaction locates the region to summarize. head is the count of leading
// messages preserved verbatim (see pinnedPrefixLen); start is where the preserved
// recent tail begins, so msgs[head:start] is compacted. The tail is bounded by a
// token budget (not a message count), so a few large tool outputs can't keep it
// above the trigger and re-fire compaction every turn. ok is false when there is
// too little to compact.
func (a *Agent) planCompaction(msgs []provider.Message, min int) (head, start int, ok bool) {
	head = a.pinnedPrefixLen(msgs)
	if a.contextWindow > 0 {
		budget := defaultTailTokens
		if maxByWin := int(float64(a.contextWindow) * defaultCompactTarget); maxByWin < budget {
			budget = maxByWin
		}
		start = tailStart(msgs, head, budget, a.tokPerChar(), a.tailFloor())
		// The calibrated character ratio is useful for cheap planning but can
		// under-estimate CJK/code-heavy turns and leave the projection above its
		// target. Re-measure the actual message estimate and move the boundary
		// forward until the retained tail itself fits the budget.
		for !a.strictAlternatingRoles && start < len(msgs)-a.tailFloor() && estimateMessagesTokens(provider.ModelMessages(msgs[start:])) > budget {
			start++
			// Never leave a tool result at the beginning of the retained tail;
			// skip the remainder of that tool-result group so its assistant call
			// is folded together with the result.
			for start < len(msgs)-a.tailFloor() && msgs[start].Role == provider.RoleTool {
				start++
			}
		}
	} else {
		// No window to budget against (manual /compact on an unconfigured
		// provider): keep a fixed count of recent messages, aligned off any tool.
		start = len(msgs) - a.tailFloor()
		for start > head && msgs[start].Role == provider.RoleTool {
			start--
		}
	}
	if start < head {
		start = head
	}
	if start-head < min {
		return head, start, false
	}
	return head, start, true
}

func (a *Agent) tailFloor() int {
	if a.recentKeep > minRecentKeep {
		return a.recentKeep
	}
	return minRecentKeep
}

// tailStart walks newest→oldest, growing the verbatim tail until the next
// message would push its token estimate past budgetTokens (but never below
// minKeep messages), then aligns the boundary back off any tool result so the
// tail never begins with an orphan whose assistant tool_calls were summarized
// away.
func tailStart(msgs []provider.Message, head, budgetTokens int, tokPerChar float64, minKeep int) int {
	start := len(msgs)
	acc := 0
	for i := len(msgs) - 1; i > head; i-- {
		c := int(float64(msgChars(msgs[i])) * tokPerChar)
		if len(msgs)-i > minKeep && acc+c > budgetTokens {
			break
		}
		acc += c
		start = i
	}
	// start == len(msgs) when nothing fit the tail (a session too small to have a
	// message after head); there is no msgs[start] to align off, and the caller's
	// minCompactMessages check then no-ops the pass.
	for start > head && start < len(msgs) && msgs[start].Role == provider.RoleTool {
		start--
	}
	return start
}

// tokPerChar derives a tokens-per-character ratio from the last turn's real
// usage so per-message estimates track the provider's tokenizer without a local
// one. Reasoning content is excluded from the char count to match the prompt
// actually sent (the provider strips it). Falls back to ~4 chars/token before
// any usage is known, and ignores absurd ratios.
func (a *Agent) tokPerChar() float64 {
	if cal := a.promptCalibration.Load(); cal != nil && cal.compactChars > 0 {
		if r := float64(cal.promptTokens) / float64(cal.compactChars); r > 0.05 && r < 2 {
			return r
		}
	}
	return fallbackTokPerChar
}

// msgChars counts the characters that ride to the provider for one message —
// content plus tool-call names and arguments, but not reasoning (stripped on
// send).
func msgChars(m provider.Message) int {
	if m.LocalOnly {
		return 0
	}
	n := len(m.Content)
	for _, tc := range m.ToolCalls {
		n += len(tc.Name) + len(tc.Arguments)
	}
	return n
}

func charsOfMessages(msgs []provider.Message) int {
	n := 0
	for _, m := range msgs {
		n += msgChars(m)
	}
	return n
}

// summarize asks the executor's own provider (no tools) to distill the region
// into a briefing. instructions is optional /compact focus + PreCompact text.
// Named returns so defer can attach RequestCount and still return usage.
func (a *Agent) summarize(ctx context.Context, region []provider.Message, instructions string) (summary string, usage *provider.Usage, err error) {
	ctx, cancel := context.WithTimeout(ctx, summaryTimeout)
	defer cancel()
	ctx = provider.WithRequestAttemptCounter(ctx)
	sys := summarySystemPrompt
	if strings.TrimSpace(instructions) != "" {
		sys += "\n\nAdditional focus for this compaction (prioritize keeping this):\n" + strings.TrimSpace(instructions)
	}
	defer func() {
		usage = provider.UsageWithRequestAttemptCount(ctx, usage)
		if usage != nil && (usage.TotalTokens > 0 || usage.RequestCount > 0) {
			a.sink.Emit(event.Event{Kind: event.Usage, ModelRef: a.modelRef, Usage: usage, Pricing: a.pricing, UsageSource: event.UsageSourceCompaction})
		}
	}()
	defer trackPublishedHostStream(ctx, cancel)()
	req := provider.Request{
		Messages: []provider.Message{
			{Role: provider.RoleSystem, Content: sys},
			{Role: provider.RoleUser, Content: renderTranscript(region)},
		},
		MaxTokens:   a.maxOutputTokens,
		Temperature: provider.OptionalTemperature(a.temperature),
	}
	if budget, clipped, budgetErr := a.effectiveOutputBudget(req); budgetErr != nil {
		return "", usage, budgetErr
	} else if clipped {
		req.MaxTokens = budget
	}
	ch, err := a.prov.Stream(ctx, req)
	if err != nil {
		return "", usage, err
	}

	// Unblock on timeout if the stream stalls while open.
	var b strings.Builder
	for {
		select {
		case <-ctx.Done():
			return "", usage, ctx.Err()
		case chunk, ok := <-ch:
			if !ok {
				if usage != nil && usage.FinishReason == "length" {
					return "", usage, fmt.Errorf("%w: provider reached the output token limit", errSummaryOutputTruncated)
				}
				s := strings.TrimSpace(b.String())
				if s == "" {
					return "", usage, fmt.Errorf("summarizer returned empty output")
				}
				return s, usage, nil
			}
			switch chunk.Type {
			case provider.ChunkText:
				b.WriteString(chunk.Text)
			case provider.ChunkUsage:
				usage = chunk.Usage
			case provider.ChunkError:
				return "", usage, chunk.Err
			}
		}
	}
}

// summarizeWithRetry retries one non-timeout failure; timeout/cancel do not retry.
// Token and request counts from both attempts are merged into the returned Usage.
func (a *Agent) summarizeWithRetry(ctx context.Context, fold []provider.Message, instructions string) (string, *provider.Usage, error) {
	summary, usage, err := a.summarize(ctx, fold, instructions)
	if err == nil || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) || errors.Is(err, errSummaryOutputTruncated) {
		return summary, usage, err
	}
	summary2, usage2, err2 := a.summarize(ctx, fold, instructions)
	return summary2, mergeStreamUsage(usage, usage2), err2
}

// renderTranscript flattens messages into a readable transcript for summarization.
func renderTranscript(msgs []provider.Message) string {
	var b strings.Builder
	for _, m := range msgs {
		if m.LocalOnly {
			continue
		}
		switch m.Role {
		case provider.RoleUser:
			fmt.Fprintf(&b, "[user]\n%s\n\n", m.Content)
		case provider.RoleAssistant:
			if m.Content != "" {
				fmt.Fprintf(&b, "[assistant]\n%s\n", m.Content)
			}
			for _, tc := range m.ToolCalls {
				fmt.Fprintf(&b, "[assistant calls %s] %s\n", tc.Name, summarizeToolArgs(tc.Arguments))
			}
			b.WriteString("\n")
		case provider.RoleTool:
			fmt.Fprintf(&b, "[tool %s result]\n%s\n\n", m.Name, m.Content)
		case provider.RoleSystem:
			fmt.Fprintf(&b, "[system]\n%s\n\n", m.Content)
		}
	}
	return b.String()
}

// summarizeToolArgs returns a short summary of tool-call arguments instead of
// the full JSON. This prevents the summarizer from reproducing long argument
// text (like sub-agent task prompts) in the compaction summary, which would
// leak into the session as a user message (#4317).
func summarizeToolArgs(args string) string {
	if args == "" {
		return "(no arguments)"
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(args), &parsed); err != nil {
		// Not valid JSON — return a length hint instead of raw text.
		return fmt.Sprintf("(%d bytes)", len(args))
	}
	keys := make([]string, 0, len(parsed))
	for k := range parsed {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return fmt.Sprintf("{%s} (%d keys)", strings.Join(keys, ", "), len(parsed))
}

// archiveMessages writes the dropped originals to a content-addressed .jsonl
// (one message per line) under dir. Retrying the same failed/stale compaction
// therefore reuses one archive instead of creating timestamp duplicates.
func archiveMessages(dir string, msgs []provider.Message) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	for _, m := range msgs {
		if err := enc.Encode(m); err != nil {
			return "", err
		}
	}
	sum := sha256.Sum256(b.Bytes())
	path := filepath.Join(dir, "context-"+hex.EncodeToString(sum[:16])+".jsonl")
	if _, err := os.Stat(path); err == nil {
		return path, nil
	} else if !os.IsNotExist(err) {
		return "", err
	}
	f, err := os.CreateTemp(dir, ".context-archive-*.tmp")
	if err != nil {
		return "", err
	}
	tmpName := f.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		return "", err
	}
	if _, err := f.Write(b.Bytes()); err != nil {
		_ = f.Close()
		return "", err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	if err := os.Link(tmpName, path); err != nil {
		if os.IsExist(err) {
			return path, nil
		}
		return "", err
	}
	_ = os.Remove(tmpName)
	cleanup = false
	return path, nil
}

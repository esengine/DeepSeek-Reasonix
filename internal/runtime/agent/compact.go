package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"reasonix/internal/state/sessionstore"
	"slices"
	"strings"
	"time"

	"reasonix/internal/contract/ablation"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
)

// Compaction is a low-frequency cache-reset point: the prompt grows append-only
// until a maintenance boundary is crossed, then one content-driven summary
// checkpoint is installed (stable prefix + one structured digest + recent tail).
// 50% is only the normal acceptance ceiling — candidates are never padded up to it.
const (
	defaultCompactRatio          = 0.85 // capacity share of the window before maintenance
	checkpointCeilingRatio       = 0.50 // normal auto-checkpoint acceptance ceiling
	recentTailBudgetRatio        = 0.10 // recent verbatim tail as a fraction of the window
	minRecentTailTokens          = 32 * 1024
	maxRecentTailTokens          = 96 * 1024
	summaryOutputMaxTokens       = 16 * 1024 // max digest output; further clipped by remaining candidate space
	summaryEffort                = "low"     // lowest depth an endpoint may approve; reasoning shares the output cap
	exceptionalMinSavingsRatio   = 0.25      // when fixed prefix alone exceeds 50%, require at least this savings
	minRecentKeep                = 2         // never keep fewer recent messages than this
	minCompactMessages           = 2         // skip compaction below this many compactable messages
	fallbackTokPerChar           = 0.25      // ~4 chars/token, used before any usage is available to calibrate
	defaultPinnedFirstUserTokens = 1500      // default ceiling on pinning the first user turn verbatim
	pinnedFirstUserWindowFrac    = 0.15      // and never pin a first turn worth more than this fraction of the window
	keptUserTurnsWindowFrac      = 0.05      // default verbatim user-turn budget, as a fraction of the window
	keptUserTurnsFloorTokens     = 1024      // ...with a floor so a small window still holds something
	protocolReserveTokens        = 256       // provider framing and control fields not represented by message estimates
)

var (
	errSummaryOutputTruncated = errors.New("summarizer output truncated")
	errCheckpointRejected     = errors.New("checkpoint candidate rejected")
)

// summaryTag wraps the compaction summary so the model can distinguish it from
// live user input and later strip or skip it when reasoning about the current turn.
const (
	SummaryTagOpen  = "<compaction-summary>"
	SummaryTagClose = "</compaction-summary>"
)

// summaryTimeout bounds one summarizer call so a stalled stream surfaces a clear
// failure (then a mechanical fold) instead of hanging compaction indefinitely.
const summaryTimeout = 90 * time.Second

// summarySystemPrompt asks for a structured resume briefing (facts, goal,
// decisions, files, commands, errors, next step) under fixed headings.
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

// compactTrigger is the sole automatic context-maintenance boundary. Output
// budgets are intentionally absent: they are clipped against the final request
// at send time and must never make compaction happen earlier than the user's
// configured compact_ratio.
// capacityCompactTrigger answers how close a prompt is to not fitting.
func (a *contextWindow) capacityCompactTrigger() int {
	window := a.effectiveContextWindow()
	if window <= 0 {
		return 0
	}
	ratio := a.compactRatio
	if ratio <= 0 {
		ratio = defaultCompactRatio
	}
	if a.ablation.Off(ablation.Compaction) {
		ratio = 0.5
	}
	return max(1, int(float64(window)*ratio))
}

// economicCompactTrigger answers what a prompt costs to replay, which is a
// property of its size and not of what the model could have held. It is an
// explicit override: an unset or negative value leaves capacity as the only
// automatic boundary.
func (a *contextWindow) economicCompactTrigger() int {
	if a.ablation.Off(ablation.Compaction) {
		return 0
	}
	limit := a.budgets.ContextSoftLimitTokens
	if limit <= 0 {
		return 0
	}
	return limit
}

// CompactTrigger is the boundary in force, which is the number a frontend has
// to show: the two bounds are configured separately and only one of them fires,
// so a panel that renders the settings alone cannot say which.
func (a *Agent) CompactTrigger() int { return a.window().compactTrigger() }

// compactTrigger is whichever boundary is reached first. An unmeasured window
// leaves both unset rather than guessing at a size, and a ratio placed past the
// window is how maintenance is turned off — economics tightens a live boundary,
// never revives a retired one.
func (a *contextWindow) compactTrigger() int {
	capacity := a.capacityCompactTrigger()
	if capacity <= 0 || a.compactRatio > 1 {
		return capacity
	}
	if economic := a.economicCompactTrigger(); economic > 0 && economic < capacity {
		return economic
	}
	return capacity
}

// hardInputCeiling is a physical input-safety boundary, not another user
// compaction threshold. Reply budgets are resolved independently at send time.
func (a *contextWindow) hardInputCeiling() int {
	window := a.effectiveContextWindow()
	if window <= 0 {
		return 0
	}
	return max(1, window-protocolReserveTokens)
}

// recentTailBudget is the content-construction budget for the recent verbatim
// tail. Production windows use clamp(window×10%, 32K, 96K). Smaller synthetic
// windows (tests / constrained providers) drop the 32K floor so the tail cannot
// alone exceed the window.
func (a *contextWindow) recentTailBudget() int {
	window := a.effectiveContextWindow()
	if window <= 0 {
		return minRecentTailTokens
	}
	n := int(float64(window) * recentTailBudgetRatio)
	if window >= minRecentTailTokens*2 {
		if n < minRecentTailTokens {
			n = minRecentTailTokens
		}
	}
	if n > maxRecentTailTokens {
		n = maxRecentTailTokens
	}
	if max := window / 2; max > 0 && n > max {
		n = max
	}
	// A tail reaching the trigger leaves nothing to fold: every message is
	// still held verbatim when the threshold arrives, so maintenance folds
	// nothing for the rest of the session. The user's threshold wins.
	if trigger := a.compactTrigger(); trigger > 0 && n > trigger/2 {
		n = trigger / 2
	}
	return max(1, n)
}

// checkpointCeiling is the auto-checkpoint acceptance upper bound: candidates
// below it are accepted without padding. The default is half the window; a
// user who would rather accept a looser fold than pay for another summary
// raises it.
func (a *contextWindow) checkpointCeiling() int {
	window := a.effectiveContextWindow()
	if window <= 0 {
		return 0
	}
	ratio := checkpointCeilingRatio
	if a.budgets.CheckpointCeilingRatio > 0 {
		ratio = a.budgets.CheckpointCeilingRatio
	}
	return max(1, int(float64(window)*ratio))
}

// exceptionalMinimumSavings is required only when the fixed prefix alone already
// exceeds the 50% ceiling; otherwise ordinary candidates simply stay under 50%.
func (a *contextWindow) exceptionalMinimumSavings() int {
	window := a.effectiveContextWindow()
	if window <= 0 {
		return 0
	}
	return max(1, int(float64(window)*exceptionalMinSavingsRatio))
}

// foldEconomics estimates whether compacting the given region saves enough
// tokens to justify the summarization API call. It returns false when the
// region is too small for the savings to outweigh the extra round-trip cost
// and latency of calling the summarizer.
func (a *contextWindow) foldEconomics(region []provider.Message) bool {
	const minFoldTokens = 100
	return a.estimatedPromptTokens(region) >= minFoldTokens
}

// SummarizeFrom keeps the compatibility index contract while installing a
// projection that compresses from that user-turn boundary onward.
func (a *Agent) SummarizeFrom(ctx context.Context, fromIdx int) error {
	return a.window().summarizeAtProjectionBoundary(ctx, fromIdx, "after")
}

// SummarizeUpTo keeps the compatibility index contract while installing a
// projection that compresses everything before that user-turn boundary.
func (a *Agent) SummarizeUpTo(ctx context.Context, toIdx int) error {
	return a.window().summarizeAtProjectionBoundary(ctx, toIdx, "before")
}

func (a *contextWindow) summarizeAtProjectionBoundary(ctx context.Context, canonicalIndex int, direction string) error {
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
		if anchor.CreatedAt == 0 && sessionstore.UserMessageText(msg) == sessionstore.UserMessageText(anchor) {
			if visibleIndex >= 0 {
				return fmt.Errorf("summarize boundary is ambiguous in the current model context")
			}
			visibleIndex = i
		}
	}
	if visibleIndex < 0 {
		return fmt.Errorf("context compression unavailable: selected turn is no longer present in the model context")
	}
	result, err := a.compressVisibleRange(ctx, snap, CompactionTriggerManual, direction, visibleIndex, anchorPreview(sessionstore.UserMessageText(anchor)), "")
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

func (a *contextWindow) activeTurnStart(msgs []provider.Message) int {
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
		strings.HasPrefix(strings.TrimLeft(m.Content, "\n "), SummaryTagOpen)
}

// pinnedPrefixLen counts the leading messages a fold keeps verbatim ahead of
// everything else: the system prompt and the first user turn (its task + stated
// facts/constraints) when it is small enough to be a brief. Digests are never
// pinned — any digest in the transcript enters the fold region and is merged
// into the next one, so a session cannot accumulate a chain of them.
func (a *contextWindow) pinnedPrefixLen(msgs []provider.Message) int {
	i := 0
	if i < len(msgs) && msgs[i].Role == provider.RoleSystem {
		i++
	}
	if i < len(msgs) && msgs[i].Role == provider.RoleUser && !isCompactionSummary(msgs[i]) && a.fixedPinnableUserTurn(msgs[i]) {
		i++
	}
	return i
}

func (a *contextWindow) fixedPinnableUserTurn(m provider.Message) bool {
	budget := defaultPinnedFirstUserTokens
	if a.budgets.FirstTurnPinTokens > 0 {
		budget = a.budgets.FirstTurnPinTokens
	}
	// The window guard stands whatever the setting: the pinned first turn sits
	// in the fixed prefix and is paid for on every request of the session.
	if window := a.effectiveContextWindow(); window > 0 {
		if f := int(float64(window) * pinnedFirstUserWindowFrac); f < budget {
			budget = f
		}
	}
	return fixedTokenEstimate(m) <= budget
}

// closedPrefixEnd is how far into msgs every tool call has its result. A fold
// may end only at such a point: anywhere else hands the provider an assistant
// turn whose calls never resolve, and hands recovery a transaction whose
// outcome the transcript no longer holds.
func closedPrefixEnd(msgs []provider.Message) int {
	open := map[string]bool{}
	end := 0
	for i, m := range msgs {
		if m.Role == provider.RoleTool && m.ToolCallID != "" {
			delete(open, m.ToolCallID)
		}
		for _, call := range m.ToolCalls {
			open[call.ID] = true
		}
		if len(open) == 0 {
			end = i + 1
		}
	}
	return end
}

func (a *contextWindow) keepIndexes(region []provider.Message) ([]bool, userTurnRetention) {
	keep := make([]bool, len(region))
	activeTurn := a.activeTurnCreatedAt.Load()
	policyStart := 0
	for i, m := range region {
		if isCompactionSummary(m) {
			policyStart = i + 1
		}
	}
	// Retention applies only to messages since the latest digest; older kept
	// messages are allowed to fold on the next pass so they cannot grow forever.
	for i, m := range region {
		if i >= policyStart && shouldKeepMessage(m, a.keepPolicy) {
			keep[i] = true
		}
		// The request that began the running turn states the work the rest of
		// the turn is executing. Its closed transactions may fold; it may not.
		if activeTurn != 0 && m.Role == provider.RoleUser && m.CreatedAt == activeTurn {
			keep[i] = true
		}
	}
	retention := a.keepUserTurns(region, keep)
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
	return keep, retention
}

// fixedTokenEstimate is what every verbatim-retention decision measures with.
// Calibrated usage must not be used here: a threshold that moves with the last
// turn's ratio would keep a turn at one checkpoint and fold it at the next.
func fixedTokenEstimate(m provider.Message) int {
	return int(float64(msgChars(m)) * fallbackTokPerChar)
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
	if m.ToolFailure != nil || failedExecution(m.ToolExecution) {
		return true
	}
	// The prefix match is what answers for a transcript written before the host
	// recorded the fact; it cannot see a failure whose words do not start that
	// way, which is why it is the fallback and not the rule.
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

// planCompaction returns [head:start] to fold; the tail is recentTailBudget
// unless force halves it so CompactNow still reduces mid-size sessions.
func (a *contextWindow) planCompaction(msgs []provider.Message, min int, force bool) (head, start int, ok bool) {
	head = a.pinnedPrefixLen(msgs)
	if a.effectiveContextWindow() > 0 {
		budget := a.recentTailBudget()
		if force {
			if half := a.estimatedPromptTokens(provider.ModelMessages(msgs)) / 2; half > 0 && half < budget {
				budget = half
			}
		}
		start = tailStart(msgs, head, budget, a.tokPerChar(), a.tailFloor())
		// Remeasure when force or non-strict roles; strict-alternating otherwise
		// keeps a cheap tokPerChar overestimate of the tail under force.
		floor := max(head, len(msgs)-a.tailFloor())
		if force || !a.strictAlternatingRoles {
			start = a.remeasuredTailStart(msgs, start, floor, budget)
		}
	} else {
		// No window: keep a fixed recent count, aligned off tool results.
		start = len(msgs) - a.tailFloor()
		for start > head && start < len(msgs) && msgs[start].Role == provider.RoleTool {
			start--
		}
	}
	start = max(start, head)
	if start-head < min {
		return head, start, false
	}
	return head, start, true
}

// remeasuredTailStart walks the cheap tokPerChar boundary forward until the
// verbatim tail fits budget under the calibrated estimate. A request's shape is
// the sum of its messages', so the measurement is carried and decremented per
// message dropped; measuring each candidate whole instead re-scanned the whole
// remaining transcript per step, and copied it whenever a receipt was present.
func (a *contextWindow) remeasuredTailStart(msgs []provider.Message, start, floor, budget int) int {
	policy := sharedWindowInputPolicyOf(a.svc.prov)
	tail := requestCalibrationShape{}
	for i := len(msgs) - 1; i >= start; i-- {
		tail = tail.plus(projectedMessageCalibrationShape(msgs[i], policy))
	}
	drop := func() {
		tail = tail.minus(projectedMessageCalibrationShape(msgs[start], policy))
		start++
	}
	for start < floor && a.estimatedShapeTokens(tail) > budget {
		drop()
		for start < floor && start < len(msgs) && msgs[start].Role == provider.RoleTool {
			drop()
		}
	}
	return start
}

func (a *contextWindow) tailFloor() int {
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
func (a *contextWindow) tokPerChar() float64 {
	if cal := a.sess.output.promptCalibration.Load(); cal != nil && cal.compactChars > 0 {
		if r := float64(cal.promptTokens) / float64(cal.compactChars); r > 0.05 && r < 2 {
			return r
		}
	}
	return fallbackTokPerChar
}

// textTokens sizes a bare string in real tokens. estimatedPromptTokens is for
// whole messages; budgeting text one line at a time, its per-message framing
// would outweigh the line.
func (a *contextWindow) textTokens(s string) int {
	return int(float64(len(s)) * a.tokPerChar())
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
func (a *contextWindow) summarize(ctx context.Context, region []provider.Message, instructions string) (summary string, usage *provider.Usage, err error) {
	parent := ctx
	ctx, cancel := context.WithTimeout(ctx, summaryTimeout)
	defer cancel()
	ctx = provider.WithRequestAttemptCounter(ctx)
	defer func() { err = classifySummaryError(parent, err) }()
	sys := summarySystemPrompt
	if strings.TrimSpace(instructions) != "" {
		sys += "\n\nAdditional focus for this compaction (prioritize keeping this):\n" + strings.TrimSpace(instructions)
	}
	defer func() {
		usage = provider.UsageWithRequestAttemptCount(ctx, usage)
		if usage != nil && (usage.TotalTokens > 0 || usage.RequestCount > 0) {
			// Recorded beside the emit, not at the call site that keeps the
			// answer: a repair whose digest is discarded was still charged.
			compactionSpendFrom(ctx).record(usage)
			a.svc.sink.Emit(event.Event{Kind: event.Usage, ModelRef: a.modelRef, Usage: usage, Pricing: a.svc.pricing, UsageSource: event.UsageSourceCompaction})
		}
	}()
	defer TrackPublishedHostStream(ctx, cancel)()
	maxOut := summaryOutputMaxTokens
	if a.maxOutputTokens > 0 && a.maxOutputTokens < maxOut {
		maxOut = a.maxOutputTokens
	}
	req := provider.Request{
		Messages: []provider.Message{
			{Role: provider.RoleSystem, Content: sys},
			{Role: provider.RoleUser, Content: renderTranscript(region)},
		},
		MaxTokens:      maxOut,
		EffortOverride: summaryEffort,
		Temperature:    provider.OptionalTemperature(a.temperature),
	}
	if budget, clipped, budgetErr := a.effectiveOutputBudget(req); budgetErr != nil {
		return "", usage, budgetErr
	} else if clipped {
		req.MaxTokens = budget
	}
	if req.MaxTokens > summaryOutputMaxTokens {
		req.MaxTokens = summaryOutputMaxTokens
	}
	if req.MaxTokens < 256 {
		return "", usage, fmt.Errorf("summary output budget too small (%d tokens)", req.MaxTokens)
	}
	if a.svc.prov == nil {
		return "", usage, fmt.Errorf("summary unavailable")
	}
	ch, err := a.svc.prov.Stream(ctx, req)
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
				// The digest is already streaming; forwarding it is what lets a
				// frontend show a fold working rather than a spinner that cannot
				// tell slow from stuck. Coalesced downstream like any delta.
				a.svc.sink.Emit(event.Event{Kind: event.CompactionProgress, Text: chunk.Text})
			case provider.ChunkUsage:
				usage = chunk.Usage
			case provider.ChunkError:
				return "", usage, chunk.Err
			}
		}
	}
}

// summarizeOnce performs exactly one application-layer summary request.
// Timeouts, empty results, stream errors, and output truncation all fail once
// with no second attempt.
func (a *contextWindow) summarizeOnce(ctx context.Context, fold []provider.Message, instructions string) (string, *provider.Usage, error) {
	return a.summarize(ctx, fold, instructions)
}

// renderTranscript flattens messages into a readable transcript for summarization.
// renderTranscript is summarizer input: tool-call arguments are summarized so
// a digest cannot reproduce a long one (#4317).
func renderTranscript(msgs []provider.Message) string {
	return renderTranscriptWith(msgs, summarizeToolArgs, modelVisibleBody)
}

// renderTranscriptVerbatim keeps the arguments. Recall exists to return what
// was actually said and run, and #4317's leak path is a digest becoming a user
// message — a recall result is a tool result, and its budget bounds the size.
func renderTranscriptVerbatim(msgs []provider.Message) string {
	return renderTranscriptWith(msgs, func(args string) string {
		if args == "" {
			return "(no arguments)"
		}
		return args
	}, fullToolBody)
}

// modelVisibleBody is the tool result the conversation actually carried. The
// full output beside it is display-only state that ModelMessages and
// ProjectionMessages both clear, and a summarizer call is a provider request.
func modelVisibleBody(m provider.Message) string { return m.Content }

// fullToolBody is what the call actually returned, for the one caller whose
// answer is that and whose own budget bounds the size.
func fullToolBody(m provider.Message) string {
	if m.RawContent != "" {
		return m.RawContent
	}
	return m.Content
}

func renderTranscriptWith(msgs []provider.Message, renderArgs func(string) string, toolBody func(provider.Message) string) string {
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
				fmt.Fprintf(&b, "[assistant calls %s] %s\n", tc.Name, renderArgs(tc.Arguments))
			}
			b.WriteString("\n")
		case provider.RoleTool:
			fmt.Fprintf(&b, "[tool %s result]\n%s\n\n", m.Name, toolBody(m))
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
	keys := slices.Sorted(maps.Keys(parsed))
	return fmt.Sprintf("{%s} (%d keys)", strings.Join(keys, ", "), len(parsed))
}

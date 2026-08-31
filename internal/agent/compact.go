package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"reasonix/internal/ablation"
	"reasonix/internal/event"
	"reasonix/internal/provider"
)

// Compaction is a low-frequency cache-reset point: the prompt grows append-only
// until compactRatio of the window is crossed, then pressure-time tool pruning
// and up to two cache-aligned summary checkpoints restore headroom.
const (
	defaultCompactRatio    = 0.80 // sole automatic maintenance trigger (new configs)
	recentTailBudgetRatio  = 0.16 // recent verbatim tail as a fraction of the window
	summaryOutputMaxTokens = 8192 // max digest output; further clipped by remaining candidate space
	minRecentKeep          = 2    // never keep fewer recent messages than this
	minCompactMessages     = 2    // skip compaction below this many compactable messages
	fallbackTokPerChar     = 0.25 // ~4 chars/token, used before any usage is available to calibrate
	protocolReserveTokens  = 256  // provider framing and control fields not represented by message estimates
)

var (
	errSummaryOutputTruncated = errors.New("summarizer output truncated")
	errCheckpointRejected     = errors.New("checkpoint candidate rejected")
)

// summaryTag wraps the compaction summary so the model can distinguish it from
// live user input and later strip or skip it when reasoning about the current turn.
const (
	summaryTagOpen  = "<compaction-summary>"
	summaryTagClose = "</compaction-summary>"
)

// compactionInstruction is appended as the only new message after an otherwise
// byte-stable sampling prefix. This lets providers reuse the ordinary request's
// system, tools and message-prefix KV cache.
const compactionInstruction = `Compact the preceding conversation prefix into a durable resume briefing.
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

Rules: be terse — bullet points and fragments, not prose. Preserve identifiers, paths, and numbers exactly. Merge valid facts from any existing <compaction-summary> and remove facts superseded by later messages. Do NOT invent anything not present in the messages; if something is unknown, leave it out rather than guessing. Output only the structured Markdown briefing. Do not call tools. Do not output reasoning.`

// compactTrigger is the sole automatic context-maintenance boundary. Output
// budgets are intentionally absent: they are clipped against the final request
// at send time and must never make compaction happen earlier than the user's
// configured compact_ratio.
func (a *Agent) compactTrigger() int {
	window := a.effectiveContextWindow()
	if a == nil || window <= 0 {
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

// hardInputCeiling is a physical input-safety boundary, not another user
// compaction threshold. Reply budgets are resolved independently at send time.
func (a *Agent) hardInputCeiling() int {
	window := a.effectiveContextWindow()
	if a == nil || window <= 0 {
		return 0
	}
	return max(1, window-protocolReserveTokens)
}

// recentTailBudget is the content-construction budget for the recent verbatim
// tail. Harness-style compaction always retains 16% of the model window.
func (a *Agent) recentTailBudget() int {
	window := a.effectiveContextWindow()
	if a == nil || window <= 0 {
		return 1
	}
	return max(1, int(float64(window)*recentTailBudgetRatio))
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
		for _, search := range m.ServerSearch {
			provider.WalkServerSearchEstimate(search, func(s string) {
				total += estimateTextTokens(s)
			})
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

// pinnedPrefixLen keeps only the system message. All older user turns,
// failures, and [[keep]] markers enter the Harness-style summary prefix.
func (a *Agent) pinnedPrefixLen(msgs []provider.Message) int {
	if len(msgs) > 0 && msgs[0].Role == provider.RoleSystem {
		return 1
	}
	return 0
}

// planCompaction returns [head:start] to fold while retaining the newest 16%
// of the model window and keeping tool-call/result groups balanced.
func (a *Agent) planCompaction(msgs []provider.Message, min int, force bool) (head, start int, ok bool) {
	head = a.pinnedPrefixLen(msgs)
	if a.contextWindow > 0 {
		budget := a.recentTailBudget()
		if force {
			if half := estimateMessagesTokens(modelInputMessages(msgs)) / 2; half > 0 && half < budget {
				budget = half
			}
		}
		start = tailStart(msgs, head, budget, a.tokPerChar(), a.tailFloor())
		// Remeasure when force or non-strict roles; strict-alternating otherwise
		// keeps a cheap tokPerChar overestimate of the tail under force.
		floor := max(head, len(msgs)-a.tailFloor())
		remeasure := force || !a.strictAlternatingRoles
		for remeasure && start < floor && estimateMessagesTokens(provider.ModelMessages(msgs[start:])) > budget {
			start++
			for start < floor && start < len(msgs) && msgs[start].Role == provider.RoleTool {
				start++
			}
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

func (a *Agent) tailFloor() int {
	return 0
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
	if cal := a.sess.output.promptCalibration.Load(); cal != nil && cal.compactChars > 0 {
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

// summarize asks the executor's own provider to distill a replayed prefix into
// a briefing. instructions is optional /compact focus + PreCompact text.
// Named returns so defer can attach RequestCount and still return usage.
func compactionInstructionWithFocus(instructions string) string {
	instruction := compactionInstruction
	if strings.TrimSpace(instructions) != "" {
		instruction += "\n\nAdditional focus for this compaction (prioritize keeping this):\n" + strings.TrimSpace(instructions)
	}
	return instruction
}

// summaryRequest builds the exact cache-aligned request shape used by
// summarize. Keeping planning and execution on this shared builder prevents a
// supposedly safe overflow fold from being rejected only after it is selected.
func (a *Agent) summaryRequest(region []provider.Message, instructions string) provider.Request {
	prefix := append([]provider.Message(nil), region...)
	if len(prefix) == 0 || prefix[0].Role != provider.RoleSystem {
		visible := a.modelVisibleMessages()
		if len(visible) > 0 && visible[0].Role == provider.RoleSystem {
			prefix = append([]provider.Message{visible[0]}, prefix...)
		}
	}
	messages := a.normalizeModelRequestMessages(prefix)
	messages = append(messages, provider.Message{Role: provider.RoleUser, Content: compactionInstructionWithFocus(instructions)})
	var schemas []provider.ToolSchema
	if a.svc.tools != nil {
		schemas = a.providerToolSchemas()
	}
	return provider.Request{
		Messages:    messages,
		Tools:       schemas,
		MaxTokens:   a.summaryOutputBudget(),
		Temperature: provider.OptionalTemperature(a.temperature),
	}
}

// summarize asks the executor's own provider to distill a replayed prefix into
// a briefing. instructions is optional /compact focus + PreCompact text.
// Named returns so defer can attach RequestCount and still return usage.
func (a *Agent) summarize(ctx context.Context, region []provider.Message, instructions string) (summary string, usage *provider.Usage, err error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	ctx = provider.WithRequestAttemptCounter(ctx)
	defer func() {
		usage = provider.UsageWithRequestAttemptCount(ctx, usage)
		if usage != nil && (usage.TotalTokens > 0 || usage.RequestCount > 0) {
			a.svc.sink.Emit(event.Event{Kind: event.Usage, ModelRef: a.modelRef, Usage: usage, Pricing: a.svc.pricing, UsageSource: event.UsageSourceCompaction})
		}
	}()
	defer trackPublishedHostStream(ctx, cancel)()
	req := a.summaryRequest(region, instructions)
	if err := a.applySummaryAdmissionToRequest(&req); err != nil {
		return "", usage, err
	}
	if budget := a.summaryOutputBudget(); req.MaxTokens > budget {
		req.MaxTokens = budget
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
func (a *Agent) summarizeOnce(ctx context.Context, fold []provider.Message, instructions string) (string, *provider.Usage, error) {
	return a.summarize(ctx, fold, instructions)
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
			body := m.Content
			if m.RawContent != "" {
				body = m.RawContent
			}
			fmt.Fprintf(&b, "[tool %s result]\n%s\n\n", m.Name, body)
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

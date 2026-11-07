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

	"reasonix/internal/contract/ablation"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
)

// Compaction is a low-frequency cache-reset point: the prompt grows append-only
// until a maintenance boundary is crossed, then one content-driven summary
// checkpoint is installed (stable prefix + one structured digest + recent tail).
// 50% is only the normal acceptance ceiling — candidates are never padded up to it.
const (
	defaultCompactRatio        = 0.90 // capacity share of the window before maintenance
	checkpointCeilingRatio     = 0.50 // normal auto-checkpoint acceptance ceiling
	refoldMinTokensRatio       = 0.10 // recent verbatim tail as a fraction of the window
	minRefoldTokens            = 32 * 1024
	maxRefoldTokens            = 96 * 1024
	exceptionalMinSavingsRatio = 0.25 // when fixed prefix alone exceeds 50%, require at least this savings
	minCompactMessages         = 2    // skip compaction below this many compactable messages
	fallbackTokPerChar         = 0.25 // ~4 chars/token, used before any usage is available to calibrate
	protocolReserveTokens      = 256  // provider framing and control fields not represented by message estimates
)

var (
	errCheckpointRejected = errors.New("checkpoint candidate rejected")
)

// summaryTag wraps the compaction summary so the model can distinguish it from
// live user input and later strip or skip it when reasoning about the current turn.
const (
	SummaryTagOpen  = "<compaction-summary>"
	SummaryTagClose = "</compaction-summary>"
)

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
	return capacity
}

// hardInputCeiling is a physical input-safety boundary, not another user
// compaction threshold. On a provider whose reply shares the window it stops
// where the send-time output check stops, so a prompt the check would refuse is
// one the fold has already been told it must free.
func (a *contextWindow) hardInputCeiling() int {
	window := a.effectiveContextWindow()
	if window <= 0 {
		return 0
	}
	reserve := protocolReserveTokens
	if sharesContextWindow(a.svc.prov) && a.configuredOutputBudget(0) > 0 {
		reserve = max(reserve, outputBudgetReserveFor(window))
	}
	return max(1, window-reserve)
}

// refoldMinTokens is how much new closed history a second fold waits for:
// every fold costs the whole prefix cache, so it must free more than it costs.
func (a *contextWindow) refoldMinTokens() int {
	window := a.effectiveContextWindow()
	if window <= 0 {
		return minRefoldTokens
	}
	n := int(float64(window) * refoldMinTokensRatio)
	if window >= minRefoldTokens*2 {
		if n < minRefoldTokens {
			n = minRefoldTokens
		}
	}
	if n > maxRefoldTokens {
		n = maxRefoldTokens
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
// user who would rather accept a looser fold than fold again sooner
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

// pinnedPrefixLen counts the leading messages a fold leaves in place: the
// system prompt and nothing else. Every conversational message may be archived.
func (a *contextWindow) pinnedPrefixLen(msgs []provider.Message) int {
	i := 0
	if i < len(msgs) && msgs[i].Role == provider.RoleSystem {
		i++
	}
	return i
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

// keepIndexes marks what a fold may not archive: the request that began the
// running turn and the tool-call groups it is still executing.
func (a *contextWindow) keepIndexes(region []provider.Message) []bool {
	keep := make([]bool, len(region))
	activeTurn := a.activeTurnCreatedAt.Load()
	for i, m := range region {
		if activeTurn != 0 && m.Role == provider.RoleUser && m.CreatedAt == activeTurn {
			keep[i] = true
		}
	}
	pinned := slices.Clone(keep)
	for i, m := range region {
		if !pinned[i] {
			continue
		}
		switch m.Role {
		case provider.RoleTool:
			if j := findToolCaller(region, i, m.ToolCallID); j >= 0 {
				keepAnsweredCall(region, keep, j)
			}
		case provider.RoleAssistant:
			keepToolCallGroup(region, keep, i)
		}
	}
	return keep
}

// fixedTokenEstimate is what every verbatim-retention decision measures with.
// Calibrated usage must not be used here: a threshold that moves with the last
// turn's ratio would keep a turn at one checkpoint and fold it at the next.
func fixedTokenEstimate(m provider.Message) int {
	return int(float64(msgChars(m)) * fallbackTokPerChar)
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

// planCompaction returns [head:start] to fold: everything after the system
// prompt. The caller trims it to the closed prefix of an active turn.
func (a *contextWindow) planCompaction(msgs []provider.Message, min int, _ bool) (head, start int, ok bool) {
	head = a.pinnedPrefixLen(msgs)
	start = len(msgs)
	if start-head < min {
		return head, start, false
	}
	return head, start, true
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

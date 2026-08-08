package agent

import (
	"context"
	"errors"
	"fmt"
	"unicode/utf8"

	"reasonix/internal/event"
	"reasonix/internal/nilutil"
	"reasonix/internal/provider"
)

const (
	fallbackTokPerChar  = 0.25 // ~4 chars/token before usage calibrates
	outputBudgetReserve = 8192 // absorbs per-message estimate drift
	minOutputBudget     = 8 * 1024
)

// ErrCompactionInputTooLarge reports that the fold to summarize cannot fit
// beside a usable output budget in the shared context window. Retrying the
// same fold cannot succeed, so callers pause auto-compaction instead of
// re-running a doomed request every turn.
var ErrCompactionInputTooLarge = errors.New("compaction input exceeds the provider context window")

// forceThreshold is the prompt high-water mark that forces compaction. On
// shared-window providers it never exceeds window - output budget - reserve,
// so a request below it cannot be rejected for exceeding the model context
// length; independent-ceiling providers keep the plain ratio mark.
func (a *Agent) forceThreshold() int {
	force := int(float64(a.contextWindow) * a.compactForceRatio)
	if sharesContextWindow(a.prov) {
		budget := a.outputBudget
		if a.maxOutputTokens > 0 {
			budget = a.maxOutputTokens
		}
		if budget > 0 {
			budgetAware := a.contextWindow - budget - 8192
			if budgetAware < force {
				force = budgetAware
			}
		}
	}
	return force
}

// estimateMessagesTokens estimates the provider-visible tokens for messages,
// including chat-message framing and tool-call structure.
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

// estimatedPromptTokens returns a conservative prompt-token estimate for the
// overflow-guard paths (preflight force, resume gate, shared-window budget
// clipping): the fixed estimator under-counts CJK transcripts (~1.8x measured),
// so the safety factor applies before usage, tokPerChar calibration after.
func (a *Agent) estimatedPromptTokens(msgs []provider.Message) int {
	est := estimateMessagesTokens(provider.ModelMessages(msgs))
	if est <= 0 {
		return 0
	}
	tpc := a.tokPerChar()
	if tpc > 0 && tpc != fallbackTokPerChar {
		// estimateMessagesTokens counts ~1 rune per token, so the calibration
		// factor is the measured ratio itself; tpc/fallback would inflate it ~4x.
		return int(float64(est) * tpc)
	}
	return est * promptEstimateSafetyFactor
}

// promptEstimateSafetyFactor is the measured under-count of the fixed CJK
// estimator before any usage calibrates it (~1.8x real, rounded up so the
// overflow guards stay inside the provider window).
const promptEstimateSafetyFactor = 2

// outputBudgetOf reads the provider's total output budget so compaction force
// thresholds can stay inside the real input allowance (context_window - output).
// Zero means the provider does not expose one (or requests omit the field).
func outputBudgetOf(p provider.Provider) int {
	if nilutil.IsNil(p) {
		return 0
	}
	if bp, ok := p.(provider.OutputBudgetProvider); ok {
		return bp.OutputBudget()
	}
	return 0
}

// sharesContextWindow reports whether the provider's output budget competes
// with the prompt input for the same context window (DeepSeek). False for
// unknown/independent-ceiling providers, keeping their default budgets intact.
func sharesContextWindow(p provider.Provider) bool {
	if nilutil.IsNil(p) {
		return false
	}
	if sp, ok := p.(provider.SharedWindowOutputProvider); ok {
		return sp.SharesContextWindow()
	}
	return false
}

// effectiveOutputBudget returns the max_output_tokens to request next round.
// Shared-window providers (DeepSeek) must keep input + output inside the
// window or the API rejects with HTTP 400, so the budget is clipped to the
// remaining allowance; (0, false) keeps the caller's default.
func (a *Agent) effectiveOutputBudget(msgs []provider.Message) (int, bool) {
	if a == nil || a.contextWindow <= 0 || !sharesContextWindow(a.prov) {
		return 0, false
	}
	// A user override wins; otherwise the provider's configured default.
	budget := a.outputBudget
	if a.maxOutputTokens > 0 {
		budget = a.maxOutputTokens
	}
	if budget <= 0 {
		return 0, false
	}
	// msgs already carries the system prompt (modelVisibleMessages includes
	// system messages), so only tool schemas are added on top.
	est := a.estimatedPromptTokens(msgs)
	if a.tools != nil {
		for _, s := range a.tools.Schemas() {
			est += estimateTextTokens(s.Name) + estimateTextTokens(s.Description) + estimateTextTokens(string(s.Parameters))
		}
	}
	return a.sharedWindowClip(budget, est)
}

// sharedWindowClip clips an output budget so budget + estimated input stays
// inside the provider's shared context window (DeepSeek rejects the sum above
// context_window with HTTP 400). Returns (0, false) to keep the caller's
// default when no clip is needed; (clipped, true) forces the smaller budget,
// never below the 8K usable floor.
func (a *Agent) sharedWindowClip(budget, est int) (int, bool) {
	if a == nil || a.contextWindow <= 0 || budget <= 0 {
		return 0, false
	}
	avail := a.contextWindow - est - outputBudgetReserve
	if budget <= avail {
		return 0, false // full budget still fits; keep the default
	}
	if avail < minOutputBudget {
		return minOutputBudget, true // never clip below a usable floor
	}
	return avail, true
}

// MaybeCompactOnResume compacts a freshly resumed session before the first
// send when the prompt cannot fit beside the output budget in the shared
// context window. Warm and within-allowance resumes stay untouched so the
// cached prefix survives (deferred-compaction policy); the canonical
// transcript is never rewritten.
func (a *Agent) MaybeCompactOnResume(ctx context.Context) {
	if a == nil || a.session == nil || a.contextWindow <= 0 {
		return
	}
	if !sharesContextWindow(a.prov) {
		return
	}
	msgs, _ := a.session.snapshotMessagesVersion()
	est := a.estimatedPromptTokens(msgs)
	// The prompt alone already leaves no room for output: any request would be
	// rejected regardless of cache state. Compact unconditionally.
	if est >= a.contextWindow-minOutputBudget-outputBudgetReserve {
		if err := a.CompactNow(ctx, ""); err == nil {
			a.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: fmt.Sprintf(
				"resumed session prompt ~%d tokens est. exceeds the shared context window's input allowance — compacted before first send", est)})
		}
	}
}

// tokPerChar derives a tokens-per-character ratio from the last turn's real
// usage so per-message estimates track the provider's tokenizer without a
// local one. Reasoning is excluded from the char count to match the prompt
// sent; absurd ratios fall back to ~4 chars/token.
func (a *Agent) tokPerChar() float64 {
	if u := a.lastUsage.Load(); u != nil && u.LatestPromptTokens() > 0 {
		// LatestPromptTokens keeps calibration on the latest single-request
		// shape: retry aggregates would over-estimate and compact too early.
		if c := int(a.lastSentChars.Load()); c > 0 {
			if r := float64(u.LatestPromptTokens()) / float64(c); r > 0.05 && r < 2 {
				return r
			}
		}
	}
	return fallbackTokPerChar
}

// msgChars counts the runes that ride to the provider for one message —
// content plus tool-call names and arguments, but not reasoning (stripped on
// send). Runes, not bytes: estimateTextTokens is also rune-based, so token
// ratios and estimates share one unit regardless of script.
func msgChars(m provider.Message) int {
	if m.LocalOnly {
		return 0
	}
	n := utf8.RuneCountInString(m.Content)
	for _, tc := range m.ToolCalls {
		n += utf8.RuneCountInString(tc.Name) + utf8.RuneCountInString(tc.Arguments)
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

package agent

import (
	"encoding/json"
	"regexp"
	"strings"
)

var reTransientUserBlock = regexp.MustCompile(`(?s)^\s*<(?:response-language|reasoning-language|memory-update|background-jobs|active-goal|hook-context|capability-route)(?:\s+[^>]*)?>.*?</(?:response-language|reasoning-language|memory-update|background-jobs|active-goal|hook-context|capability-route)>\s*\n?`)

// stripTrailingDeliveryRuntime removes the exact delivery-runtime marker the
// agent appends to user turns in delivery mode (agent.go DeliveryRuntimeMarker).
// Unlike the prefix blocks it trails the user text, so preview/title derivation
// needs a suffix cut — leaving it produced session titles like
// "你是谁？ <delivery-run…". The cut is byte-exact rather than a regex: a lazy
// pattern anchored at $ would swallow user prose between a literal
// "<delivery-runtime>" mention in the text and the real marker at the end.
// (The agent never appends the marker when the input already mentions the tag,
// so user messages discussing it carry no host suffix at all.)
func stripTrailingDeliveryRuntime(s string) string {
	trimmed := strings.TrimRight(s, " \t\r\n")
	if cut, ok := strings.CutSuffix(trimmed, DeliveryRuntimeMarker); ok {
		return strings.TrimRight(cut, " \t\r\n")
	}
	return s
}

const memoryCompilerExecutionOpen = "<memory-compiler-execution>"

var reMemoryCompilerExecution = regexp.MustCompile(`(?s)<memory-compiler-execution>\s*(.*?)\s*</memory-compiler-execution>`)

// ContainsMemoryCompilerExecution reports whether content includes a Memory v5
// execution contract. Callers that prepare user-facing or replayable text should
// unwrap it before display and avoid treating the raw contract as user-authored.
func ContainsMemoryCompilerExecution(content string) bool {
	return strings.Contains(content, memoryCompilerExecutionOpen)
}

// StripTransientUserBlocks removes controller-injected transient XML blocks
// from persisted user messages before deriving display text, previews, or
// titles. The blocks are sent in user turns so they never affect the stable
// prompt prefix, but they should not become user-facing text later.
//
// The Memory v5 <memory-compiler-execution> block is handled differently from
// the prepended transient blocks: it does not prefix the user's prompt, it
// REPLACES the whole turn (Agent.Run swaps the compiled contract in for the
// original input, keeping the user's text only in the contract's source_event
// field). Dropping it like a prefix block would leave an empty string, so we
// unwrap it to the original prompt instead — otherwise sessions whose first
// turn was compiled would show a blank history/sidebar preview (#5307).
func StripTransientUserBlocks(content string) string {
	s := unwrapMemoryCompilerExecution(content)
	for {
		next := reTransientUserBlock.ReplaceAllStringFunc(s, func(string) string {
			return ""
		})
		if next == s {
			break
		}
		s = next
	}
	s = stripTrailingDeliveryRuntime(s)
	return strings.TrimLeft(s, " \t\r\n")
}

// unwrapMemoryCompilerExecution replaces a <memory-compiler-execution> contract
// with the user prompt it was compiled from (the contract's source_event), so
// display text and previews show what the user typed rather than the raw IR
// JSON or an empty string. Non-contract content is returned unchanged; a
// contract without a recoverable source_event collapses to empty, matching the
// prior "strip the block" behavior only as a last resort.
func unwrapMemoryCompilerExecution(content string) string {
	// Unwrap to a fixpoint. A long goal loop (the #5342 bug) could re-compile an
	// echoed contract many times, so source_event nests another full
	// <memory-compiler-execution> block; each pass peels the outermost layer and
	// exposes the next. A single (or fixed two) pass leaves raw contract JSON in
	// the transcript (#5361). maxDepth bounds pathological accretion.
	const maxDepth = 24
	for range maxDepth {
		if !ContainsMemoryCompilerExecution(content) {
			return content
		}
		next := reMemoryCompilerExecution.ReplaceAllStringFunc(content, func(block string) string {
			m := reMemoryCompilerExecution.FindStringSubmatch(block)
			if len(m) < 2 {
				return ""
			}
			return memoryCompilerSourceEvent(m[1])
		})
		if next == content {
			break // no complete block matched (e.g. a dangling/truncated tag)
		}
		content = next
	}
	// Any residual open tag is a dangling/partial/unparseable block the strict
	// regex can't complete; drop from the first open tag onward so raw contract
	// JSON is never surfaced. The user's actual text precedes it.
	if idx := strings.Index(content, memoryCompilerExecutionOpen); idx >= 0 {
		content = strings.TrimRight(content[:idx], " \t\r\n")
	}
	return content
}

// memoryCompilerSourceEvent pulls the original user prompt out of a compiled
// execution contract's JSON body. The source_event lives under planner_ir; an
// older/looser shape may carry it at the top level, so both are checked.
// Returns "" when the body is not the expected JSON or carries no source_event.
func memoryCompilerSourceEvent(body string) string {
	var contract struct {
		SourceEvent string `json:"source_event"`
		PlannerIR   struct {
			SourceEvent string `json:"source_event"`
		} `json:"planner_ir"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(body)), &contract); err != nil {
		return ""
	}
	if s := strings.TrimSpace(contract.PlannerIR.SourceEvent); s != "" {
		return s
	}
	return strings.TrimSpace(contract.SourceEvent)
}

// UserPreviewText returns the user-authored part of a persisted user message.
func UserPreviewText(content string) string {
	s := StripTransientUserBlocks(content)
	s = HandoffTask(s)
	s = StripTransientUserBlocks(s)
	return strings.TrimSpace(s)
}

// SyntheticUserPrefixes lists the openings of host-injected user-role messages
// (readiness retries, stream recovery, goal-loop nudges, compaction folds).
// They are persisted with role "user" for provider-contract reasons but are not
// user-authored: previews, titles, and user-turn counts must skip them, and the
// chat UI never renders them as user bubbles. Keep in sync with the injection
// sites in internal/agent/agent.go, internal/agent/compact.go, and
// internal/control (plan approval, goal loop).
var SyntheticUserPrefixes = []string{
	"Plan approved — plan mode is off",
	"Host final-answer readiness check failed",
	"You are already in the executor phase",
	"The previous assistant response was interrupted while a tool call",
	"The previous assistant response was interrupted during streaming",
	"The previous assistant response was interrupted before visible",
	"The previous assistant response finished without any visible answer",
	"<compaction-summary>",
	"Summary of the later conversation (compacted from here on):",
	"Summary of earlier conversation (compacted up to here):",
	"Continue pursuing the active goal",
	"The agent signaled goal completion and all tasks are marked done.",
	"Goal signaled complete but issues remain:",
	"No tool calls in recent turns.",
}

// IsSyntheticUserText reports whether a persisted user-role message is a
// host-injected synthetic turn rather than user-authored input.
func IsSyntheticUserText(content string) bool {
	trimmed := strings.TrimSpace(StripTransientUserBlocks(content))
	for _, prefix := range SyntheticUserPrefixes {
		if strings.HasPrefix(trimmed, prefix) {
			return true
		}
	}
	return false
}

// IsUserAuthoredTurn reports whether a persisted user-role message counts as a
// visible user turn: not a host-injected synthetic message and not a mid-turn
// steer. Preview/title/turn-count derivations share this so a delivery
// readiness nudge can never become a session title or inflate turn counts.
func IsUserAuthoredTurn(content string) bool {
	if IsSyntheticUserText(content) {
		return false
	}
	if _, isSteer := SteerText(content); isSteer {
		return false
	}
	return true
}

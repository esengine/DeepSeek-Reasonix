package agent

import (
	"encoding/json"
	"regexp"
	"strings"

	"reasonix/internal/provider"
)

// TransientUserBlockTags names every block the host prepends to a user turn as
// runtime context rather than something the user typed. Previews, titles, and
// the rewind picker strip them; a tag missing from this list leaks raw markup
// into the UI, which is how <autoresearch-runtime> surfaced in session titles.
//
// This is the single source of truth: the strip regex is built from it, and
// hasLeadingInjectedBlock walks it. Anything that starts prepending a new block
// to user turns belongs here.
var TransientUserBlockTags = []string{
	"response-language",
	"reasoning-language",
	"memory-update",
	"background-jobs",
	"active-goal",
	"autoresearch-runtime",
	"hook-context",
	"available-skills",
	"project-instructions",
	"capability-route",
	"interrupted-turn-recovery",
	"execution-policy",
	"workspace",
}

// SupersededUserBlockTags names the transient blocks carrying standing state
// rather than a one-time fact: the live turn always restates them, so an older
// copy in retained history is redundant. A block reporting something that
// happened once — a memory write, a finished job, the policy that governed one
// turn — is absent on purpose: dropping those destroys the only record of it.
var SupersededUserBlockTags = []string{
	"active-goal",
	"response-language",
	"reasoning-language",
	"workspace",
	"project-instructions",
}

// supersededUserBlock indexes SupersededUserBlockTags so the two cannot drift.
var supersededUserBlock = func() map[string]bool {
	m := make(map[string]bool, len(SupersededUserBlockTags))
	for _, tag := range SupersededUserBlockTags {
		m[tag] = true
	}
	return m
}()

var reTransientUserBlock = buildTransientUserBlockRE(TransientUserBlockTags)

// buildTransientUserBlockRE matches one leading transient block: an open tag
// (with optional attributes), its content, and its own closing tag. The
// alternation is generated so the open and close lists cannot drift apart —
// spelling them out twice by hand is what let tags go missing from one side.
func buildTransientUserBlockRE(tags []string) *regexp.Regexp {
	alt := strings.Join(tags, "|")
	return regexp.MustCompile(`(?s)^\s*<(?:` + alt + `)(?:\s+[^>]*)?>.*?</(?:` + alt + `)>\s*\n?`)
}

// stripTrailingDeliveryRuntime removes the retired delivery-runtime marker
// (agent.go DeliveryRuntimeMarker) from sessions recorded while it was still
// appended. Unlike the prefix blocks it trails the user text, so preview/title
// derivation needs a suffix cut — leaving it produced session titles like
// "你是谁？ <delivery-run…". The cut is byte-exact rather than a regex: a lazy
// pattern anchored at $ would swallow user prose between a literal
// "<delivery-runtime>" mention in the text and the real marker at the end.
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
// execution contract. The Memory v5 compiler was removed, but transcripts
// recorded by releases up to v1.17.x may still carry injected contracts in
// persisted user messages, so display paths keep unwrapping them. Callers that
// prepare user-facing or replayable text should unwrap the block before display
// and avoid treating the raw contract as user-authored.
func ContainsMemoryCompilerExecution(content string) bool {
	return strings.Contains(content, memoryCompilerExecutionOpen)
}

// StripTransientUserBlocks removes controller-injected transient XML blocks
// from persisted user messages before deriving display text, previews, or
// titles. The blocks are sent in user turns so they never affect the stable
// prompt prefix, but they should not become user-facing text later.
//
// The legacy Memory v5 <memory-compiler-execution> block (written by releases
// up to v1.17.x before the compiler was removed) is handled differently from
// the prepended transient blocks: it did not prefix the user's prompt, it
// REPLACED the whole turn, keeping the user's text only in the contract's
// source_event field. Dropping it like a prefix block would leave an empty
// string, so we unwrap it to the original prompt instead — otherwise old
// sessions whose first turn was compiled would show a blank history/sidebar
// preview (#5307).
func StripTransientUserBlocks(content string) string {
	s := DropLeadingTransientBlocks(unwrapMemoryCompilerExecution(content))
	s = stripTrailingDeliveryRuntime(s)
	s = stripTrailingExecutionPolicy(s)
	s = stripTrailingMemoryRecall(s)
	return strings.TrimLeft(s, " \t\r\n")
}

// DropLeadingTransientBlocks removes the host-injected blocks that open s. The
// pattern is ^-anchored, so a ReplaceAll can only ever take the first one and
// looping around it re-scans the whole string once per block; advancing past
// each match reads the content once and allocates nothing. Exported because the
// history index strips the same blocks from the same turns.
func DropLeadingTransientBlocks(s string) string {
	for {
		loc := reTransientUserBlock.FindStringIndex(s)
		if loc == nil || loc[0] != 0 {
			return s
		}
		s = s[loc[1]:]
	}
}

// stripTrailingExecutionPolicy removes the host-appended execution-policy block
// from the end of a user turn. Byte-exact for the reason the delivery-runtime
// cut is: a lazy pattern anchored at $ swallows user prose between a literal
// mention and the real block, and on a long turn it backtracks over the whole
// string for every position it tries.
func stripTrailingExecutionPolicy(s string) string {
	trimmed := strings.TrimRight(s, " \t\r\n")
	const close = "</execution-policy>"
	if !strings.HasSuffix(trimmed, close) {
		return s
	}
	// The opening tag carries attributes, so match its head rather than a
	// literal, and take the last one: the block is the suffix.
	if index := strings.LastIndex(trimmed, "<execution-policy"); index >= 0 {
		return strings.TrimRight(trimmed[:index], " \t\r\n")
	}
	return s
}

func stripTrailingMemoryRecall(s string) string {
	trimmed := strings.TrimRight(s, " \t\r\n")
	const open = "<memory-recall>"
	const close = "</memory-recall>"
	if !strings.HasSuffix(trimmed, close) {
		return s
	}
	if index := strings.LastIndex(trimmed, open); index >= 0 {
		return strings.TrimRight(trimmed[:index], " \t\r\n")
	}
	return s
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

// pasteDisplayLabelPattern matches the standalone label desktop prepends to a
// pasted-text turn. It is UI chrome rather than user intent, so title and
// preview derivation may remove it without touching inline label mentions.
var pasteDisplayLabelPattern = regexp.MustCompile(`^\[(?:已粘贴文本|已貼上文字|Pasted text) #[0-9]+ · [0-9]+ (?:行|lines)\][ \t]*(?:\r?\n)?`)

// StripPasteDisplayLabel removes one leading desktop pasted-text label while
// preserving the remainder byte-for-byte.
func StripPasteDisplayLabel(content string) string {
	return pasteDisplayLabelPattern.ReplaceAllString(content, "")
}

// UserMessageText returns the best user-authored view of a persisted user turn.
// New sessions carry the exact raw text explicitly; older sessions fall back to
// deterministic wrapper stripping.
func UserMessageText(msg provider.Message) string {
	if msg.RawContent != "" {
		// Stripped here too: RawContent is meant to hold what a person typed,
		// but a host-authored turn writes its own composed text to it, and
		// returning that verbatim put <background-jobs> in the transcript.
		return strings.TrimSpace(StripTransientUserBlocks(msg.RawContent))
	}
	return UserPreviewText(msg.Content)
}

// migrateLegacyProviderContent canonicalizes both historical user-turn shapes:
// legacy turns kept provider-visible text only in Content, while early Context
// Engine v2 builds inverted Content and ProviderContent. Canonical sessions
// keep provider-visible bytes in Content so previous releases replay them
// safely, with user-authored text in RawContent for current display/search.
func migrateLegacyProviderContent(msgs []provider.Message) []provider.Message {
	var upgraded []provider.Message
	for i, msg := range msgs {
		if msg.Role != provider.RoleUser {
			continue
		}
		switch {
		case msg.ProviderContent != "":
			if upgraded == nil {
				upgraded = append([]provider.Message(nil), msgs...)
			}
			if upgraded[i].RawContent == "" {
				upgraded[i].RawContent = msg.Content
			}
			upgraded[i].Content = msg.ProviderContent
			upgraded[i].ProviderContent = ""
		case msg.RawContent == "" && hasLegacyProviderWrapper(msg.Content):
			if upgraded == nil {
				upgraded = append([]provider.Message(nil), msgs...)
			}
			upgraded[i].RawContent = UserPreviewText(msg.Content)
		}
	}
	if upgraded != nil {
		return upgraded
	}
	return msgs
}

func hasLegacyProviderWrapper(content string) bool {
	if ContainsMemoryCompilerExecution(content) || reTransientUserBlock.MatchString(content) {
		return true
	}
	if stripTrailingDeliveryRuntime(content) != content {
		return true
	}
	stripped := StripTransientUserBlocks(content)
	return HandoffTask(stripped) != stripped
}

// SyntheticUserPrefixes lists the openings of host-injected user-role messages
// (readiness retries, stream recovery, goal-loop nudges, compaction folds).
// They are persisted with role "user" for provider-contract reasons but are not
// user-authored: previews, titles, and user-turn counts must skip them, and the
// chat UI never renders them as user bubbles. Keep in sync with the injection
// sites in internal/agent/agent.go, internal/agent/compact.go, and
// internal/control (plan approval, goal loop).
var SyntheticUserPrefixes = []string{
	"<reasoning-language>",
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
	if strings.TrimSpace(StripTransientUserBlocks(content)) == "" {
		return false
	}
	if IsSyntheticUserText(content) {
		return false
	}
	if _, isSteer := SteerText(content); isSteer {
		return false
	}
	return true
}

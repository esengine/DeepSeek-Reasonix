package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"reasonix/internal/contract/provider"
)

// responseBoundary is one sampling terminal classified against the tool calls
// it carried. Truncation can only have cut the *last* call, so an unparseable
// tail is a host fact there and the model's own error anywhere else.
type responseBoundary struct {
	committed []provider.ToolCall
	dropped   string // the incomplete trailing call, empty when none was dropped
	fact      string // host attribution owed to the model, empty on a normal terminal
}

// classifyResponseBoundary decides what a sampling terminal may commit. It runs
// before the assistant turn reaches the conversation: committing a half-written
// call replays it on every later request, and executing it spends a round on
// arguments the model never finished. A cut that landed after the last call
// closed drops nothing — "cut short" is not "every call in it is suspect".
func classifyResponseBoundary(usage *provider.Usage, calls []provider.ToolCall) responseBoundary {
	if len(calls) == 0 {
		return responseBoundary{committed: calls}
	}
	last := len(calls) - 1
	reported := truncatedTerminal(usage)
	// Arguments ending mid-value are the host's own evidence of a cut, and do
	// not depend on the finish reason arriving. A clean terminal alongside a
	// half-written call reached execution and returned "invalid JSON".
	cut := argumentsCutOff(calls[last].Arguments)
	if !reported && !cut {
		return responseBoundary{committed: calls}
	}
	reason := ""
	if usage != nil {
		reason = usage.FinishReason
	}
	if !cut && json.Valid([]byte(calls[last].Arguments)) {
		return responseBoundary{committed: calls, fact: truncationFact(reason, len(calls), "")}
	}
	return responseBoundary{
		committed: calls[:last],
		dropped:   calls[last].Name,
		fact:      truncationFact(reason, last, calls[last].Name),
	}
}

// argumentsCutOff reports arguments that stop mid-value: the decoder ran out of
// input inside a token, which no syntax error and no complete document does.
// Empty arguments are a valid call shape, not a cut.
func argumentsCutOff(arguments string) bool {
	if strings.TrimSpace(arguments) == "" {
		return false
	}
	var probe any
	err := json.NewDecoder(strings.NewReader(arguments)).Decode(&probe)
	return errors.Is(err, io.ErrUnexpectedEOF)
}

// truncatedTerminal reports the finish reasons that end a response before the
// model chose to stop. A content filter is not one: it refused what was
// written rather than leaving it unfinished.
func truncatedTerminal(u *provider.Usage) bool {
	if u == nil {
		return false
	}
	switch u.FinishReason {
	case "length", "repetition_truncation":
		return true
	}
	return false
}

// truncationCause reads a finish reason as a clause. The reason is the
// identity; this is only its wording.
func truncationCause(reason string) string {
	switch reason {
	case "repetition_truncation":
		return "was stopped by the host's repetition guard"
	default:
		return "hit the model's output-token limit"
	}
}

// truncationFact carries what only the host knows: the response ended at a
// limit, not where the model meant it to. Given just the downstream symptom —
// a call missing required arguments — a session instead learns to stop
// batching calls that were never the problem.
func truncationFact(reason string, executed int, dropped string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Host fact about your previous response, not a mistake in your calls: it %s and was cut off.", truncationCause(reason))
	if dropped != "" {
		fmt.Fprintf(&b, "\n\nThe trailing tool call %q was still being written when the cut landed, so the host dropped it: it did not run, its half-written arguments were not repaired or guessed at, and it is not in the conversation. Any error you would expect from it does not exist.", dropped)
	}
	switch executed {
	case 0:
		b.WriteString("\n\nNothing from that response ran.")
	case 1:
		b.WriteString("\n\nThe 1 tool call that was already complete ran normally; its result is above.")
	default:
		fmt.Fprintf(&b, "\n\nThe %d tool calls that were already complete ran normally; their results are above.", executed)
	}
	b.WriteString("\n\nRe-emit only the work that did not run. Batching independent calls in one response is correct and stays correct — this was a size limit, not a batching failure — but keep this response within it: fewer or shorter arguments per response, and split a long write across calls.")
	return b.String()
}

// noticeDetail gives the frontend warning the same attribution the model got.
func (b responseBoundary) noticeDetail() string {
	if b.dropped == "" {
		return ""
	}
	return fmt.Sprintf("trailing tool call %q was incomplete and was not executed", b.dropped)
}

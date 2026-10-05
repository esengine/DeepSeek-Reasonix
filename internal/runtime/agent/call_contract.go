package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reasonix/internal/runtime/usecap"
	"strings"
	"unicode/utf8"
)

// applyCallContract refuses a call the target's own schema already rules out: a
// required field omitted, or one filled with a kind the schema does not admit.
// Execution was never possible, so this keeps a user approval from being spent
// on a call that can only fail after it, and keeps the answer from being an
// unmarshal error written in the host language's vocabulary.
func (a *Agent) applyCallContract(plan *toolCallPlan) (toolOutcome, bool) {
	if plan.execTool == nil {
		return toolOutcome{}, false
	}
	contract, ok := usecap.ReadArgumentContract(plan.execTool.Schema(), plan.execArgs)
	if !ok || (len(contract.Missing) == 0 && len(contract.Mistyped) == 0) {
		return toolOutcome{}, false
	}
	msg := fmt.Sprintf("invalid arguments for %s%s", plan.permName, contract.Hint())
	return toolOutcome{output: "error: " + msg, errMsg: msg}, true
}

// malformedArgumentsDetail says why the arguments did not parse. A call that
// was cut off mid-value and one that mistyped an escape need opposite fixes —
// send less, versus escape correctly — and a bare "not valid JSON" sends both
// to the wrong one.
func malformedArgumentsDetail(arguments string) string {
	const reemit = " Re-emit them exactly per this schema:"
	var probe any
	err := json.NewDecoder(strings.NewReader(arguments)).Decode(&probe)
	switch syntax := (*json.SyntaxError)(nil); {
	case errors.Is(err, io.ErrUnexpectedEOF):
		return "The arguments end mid-value: they were cut off, not mistyped, so re-sending the same shape truncates again. Keep summaries and free text short." + reemit
	case errors.As(err, &syntax):
		return fmt.Sprintf("The arguments were not valid JSON at byte %d: %v.%s%s",
			syntax.Offset, err, argumentsExcerpt(arguments, syntax.Offset), reemit)
	}
	return "The arguments were not valid JSON." + reemit
}

// argumentsExcerpt shows the bytes the parser stopped on. An offset into a
// payload the model cannot see leaves it explaining the failure with whatever
// it *can* see — which is how a missing delimiter is learned as "the host
// rejects angle brackets". The closing line says what the caret does and does
// not prove, so the excerpt cannot teach a rule of its own.
func argumentsExcerpt(arguments string, offset int64) string {
	at := int(offset) - 1 // Offset counts bytes consumed, so the byte read last precedes it
	at = min(max(at, 0), len(arguments)-1)
	if at < 0 {
		return ""
	}
	const flank = 48
	lo, hi := max(at-flank, 0), min(at+flank+1, len(arguments))
	for lo > 0 && !utf8.RuneStart(arguments[lo]) {
		lo--
	}
	for hi < len(arguments) && !utf8.RuneStart(arguments[hi]) {
		hi++
	}

	var line strings.Builder
	if lo > 0 {
		line.WriteString("…")
	}
	caret := -1
	for i := lo; i < hi; {
		r, size := utf8.DecodeRuneInString(arguments[i:])
		if caret < 0 && i <= at && at < i+size {
			caret = visibleWidth(line.String())
		}
		line.WriteString(escapeExcerptRune(r))
		i += size
	}
	if hi < len(arguments) {
		line.WriteString("…")
	}
	if caret < 0 {
		caret = visibleWidth(line.String())
	}
	return fmt.Sprintf("\n\n  %s\n  %s^\n\nThe caret marks where parsing stopped, not necessarily the character to remove: any byte is legal inside a JSON string, so when one shows up in the syntax around the strings it is the quoting or a delimiter before it that is missing.",
		line.String(), strings.Repeat(" ", caret))
}

// escapeExcerptRune keeps an excerpt to one line so the caret below it stays
// aligned with the byte it points at.
func escapeExcerptRune(r rune) string {
	switch r {
	case '\n':
		return "\\n"
	case '\r':
		return "\\r"
	case '\t':
		return "\\t"
	}
	if r < 0x20 || r == 0x7f {
		return fmt.Sprintf("\\x%02x", r)
	}
	return string(r)
}

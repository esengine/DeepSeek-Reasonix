package evidence

import (
	"encoding/json"
	"strings"

	"reasonix/internal/base/shellparse"
)

// unprovenSegmentLimit bounds the segment a block quotes back. A whole command
// echoed into the transcript is noise; the offending head of it is the fix.
const unprovenSegmentLimit = 120

// bashUnprovenSegment returns the first segment the host cannot prove leaves the
// workspace alone. Not proving it is not the same as knowing it writes: an
// unrecognized command is simply outside what the host can classify, and the
// caller must stay conservative either way.
func bashUnprovenSegment(command string) (string, bool) {
	class, segment := bashScanMutation(command)
	return segment, class != MutationNone
}

func bashMayMutate(command string) bool {
	return bashMutationClass(command) != MutationNone
}

// UnprovenSegment returns the one segment of command the static tables could
// not prove leaves the workspace alone. Callers that can escalate — ask a model,
// consult a cache — need the segment itself, not just the verdict; the tables
// will never enumerate every tool, and a caller stuck at "something here might
// write" has nothing to escalate about.
func UnprovenSegment(command string) (string, bool) {
	return bashUnprovenSegment(command)
}

// ShellContractMixedMessage is the "mixed" block, naming the segment that
// triggered it. Without the name the model rewrites the command it guesses is
// at fault, which is how one block becomes three.
func ShellContractMixedMessage(args json.RawMessage) string {
	segment := namedUnprovenSegment(args)
	if segment == "" {
		return ShellContractPreflightMessage("mixed")
	}
	return "blocked: this command runs a verification check after `" + segment +
		"`, which the host cannot prove leaves the workspace alone, and the check's exit status " +
		"would hide a failure in it. Chain them with '&&' so a failed step stops the command and " +
		"stays the result, or run that segment and the verification as separate calls."
}

// ShellContractMixedDeliveryMessage names the segment the same way, but not the
// '&&' way out: Delivery refuses the mixture whatever the exit status does, so
// sending a run there earns it the same block again. One observed delivery run
// was refused three times in a row by the unnamed version of this message.
func ShellContractMixedDeliveryMessage(args json.RawMessage) string {
	segment := namedUnprovenSegment(args)
	if segment == "" {
		return ShellContractPreflightMessage("mixed_delivery")
	}
	return "blocked: `" + segment + "` is a segment the host cannot prove leaves the workspace " +
		"alone, and delivery keeps a verification call free of anything else — chaining it with " +
		"'&&' is refused too, because the receipt has to be a check and nothing besides. Run that " +
		"segment as its own call while a todo is in_progress, then run the verification by itself."
}

// ShellContractInlineNonTerminalMessage is the "inline_nonterminal" block,
// naming both the interpreter the host cannot audit and the segment that takes
// over its exit status. '&&' is the only separator that keeps that status, so
// the way out has to say which ones do not.
func ShellContractInlineNonTerminalMessage(args json.RawMessage) string {
	inline, follower := namedInlineNonTerminalPair(args)
	if inline == "" {
		return ShellContractPreflightMessage("inline_nonterminal")
	}
	return "blocked: `" + inline + "` is an inline interpreter the host cannot audit, and it is " +
		"not the last segment — `" + follower + "` and whatever follows it decide the command's " +
		"exit status, so a failure inside the interpreter would read as success. Chain with '&&' " +
		"and put nothing after the interpreter — ';' and '||' hand the status on the same way — " +
		"or run it by itself."
}

// namedUnprovenSegment returns the offending segment, bounded for display.
func namedUnprovenSegment(args json.RawMessage) string {
	command, ok := bashCommandFromArgs(args)
	if !ok {
		return ""
	}
	found, unproven := bashUnprovenSegment(command)
	if !unproven {
		return ""
	}
	return boundedSegment(found)
}

// namedInlineNonTerminalPair returns the opaque interpreter segment and the one
// that follows it — the pair the block is about — both bounded for display.
func namedInlineNonTerminalPair(args json.RawMessage) (string, string) {
	command, ok := bashCommandFromArgs(args)
	if !ok {
		return "", ""
	}
	segments, _, ok := shellparse.SplitTopLevel(command)
	if !ok || len(segments) < 2 {
		return "", ""
	}
	for i, segment := range segments[:len(segments)-1] {
		if bashSegmentUsesOpaqueInlineInterpreter(segment) {
			return boundedSegment(segment), boundedSegment(segments[i+1])
		}
	}
	return "", ""
}

func boundedSegment(segment string) string {
	segment = strings.TrimSpace(segment)
	if len(segment) > unprovenSegmentLimit {
		segment = segment[:unprovenSegmentLimit] + "…"
	}
	return segment
}

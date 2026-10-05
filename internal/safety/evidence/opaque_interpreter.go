// Whether a bash call hands its program to an interpreter instead of naming a
// file the host can read. The answer is the same one permission reads; nothing
// here decides whether the call may run.
package evidence

import (
	"encoding/json"
	"slices"

	"reasonix/internal/base/shellparse"
	"reasonix/internal/safety/shellsafe"
)

// BashToolCallUsesOpaqueInlineInterpreter reports whether a bash call runs
// source it carries rather than a file. The host cannot tell whether such a
// snippet only read state or also wrote it, so the change it may have made is
// recorded unproven and owed at final readiness — not refused here.
func BashToolCallUsesOpaqueInlineInterpreter(args json.RawMessage) bool {
	command, ok := bashCommandFromArgs(args)
	if !ok {
		return false
	}
	return bashCommandUsesOpaqueInlineInterpreter(command)
}

// BashCommandMayBeOpaqueMutation reports whether a sole opaque inline
// interpreter call is allowed to run but cannot be proven read-only for
// mutation-risk labeling.
func BashCommandMayBeOpaqueMutation(args json.RawMessage) bool {
	return BashToolCallUsesOpaqueInlineInterpreter(args)
}

func bashCommandUsesOpaqueInlineInterpreter(command string) bool {
	segments, _, ok := shellparse.SplitTopLevel(command)
	if ok && slices.ContainsFunc(segments, bashSegmentUsesOpaqueInlineInterpreter) {
		return true
	}
	// `python - <<PY` hands its program over as -c does, and a here-doc costs the
	// split above the whole command.
	return slices.ContainsFunc(shellparse.StdinHereDocArgv(command), shellsafe.ArgvTakesProgramFromStdin)
}

func bashSegmentUsesOpaqueInlineInterpreter(segment string) bool {
	normalized, _ := shellsafe.NormalizeBashSafeRedirectsForMatch(segment)
	argv, malformed := shellparse.StaticFields(normalized)
	if malformed != "" {
		return false
	}
	return shellsafe.ArgvCarriesInlineCode(argv)
}

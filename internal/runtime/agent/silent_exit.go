package agent

import (
	"encoding/json"
	"strings"

	"reasonix/internal/safety/evidence"
)

// silentExitIsAnAnswer reports whether a non-zero exit reported something rather
// than broke: the call printed nothing, the host proved it changed nothing, and
// it carries no verification conclusion. grep, `test -f` and `git diff --quiet`
// all report absence that way, and a run told only "error" re-runs the same
// search — six times, in one observed run — because it never learned the answer.
func silentExitIsAnAnswer(toolName string, args json.RawMessage, output string) bool {
	if toolName != "bash" || strings.TrimSpace(output) != "" {
		return false
	}
	command := bashCommandFromArgs(args)
	if command == "" || evidence.CommandRunsVerification(command) {
		return false
	}
	return evidence.ToolCallMutationClass(toolName, args, false) == evidence.MutationNone
}

// silentSuccessDetail is what a call that succeeded and printed nothing says.
// An empty result reads the same as a broken one — a run told neither re-runs
// the same search, which is the case silentExitNote already exists for on the
// failing side. Anything that printed something, or that may have written, is
// returned unchanged.
func silentSuccessDetail(toolName string, args json.RawMessage, output string) string {
	if silentExitIsAnAnswer(toolName, args, output) {
		return silentExitNote
	}
	return output
}

// silentExitDetail is the body a failed call carries below its error line: the
// tool's own output, or the note that stands in for the nothing it printed.
func silentExitDetail(toolName string, args json.RawMessage, output string) string {
	if silentExitIsAnAnswer(toolName, args, output) {
		return silentExitNote
	}
	return output
}

const silentExitNote = "The command printed nothing and the host proved it changed nothing, so this status is what it found: for a search or a file test, that means no match."

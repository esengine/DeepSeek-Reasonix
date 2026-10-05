package agent

import (
	"encoding/json"
	"strings"

	"reasonix/internal/base/shellparse"
	"reasonix/internal/contract/tool"
	"reasonix/internal/safety/evidence"
)

// shellVerificationVerdict reads a verification's outcome off what the host
// actually knows: bash's own per-stage statuses when it reported them, then the
// exit status when it can only belong to the check, and inconclusive when a
// later stage of the same command could have decided it instead.
func shellVerificationVerdict(command string, declared bool, pipeStatus []int, err error) string {
	switch evidence.VerificationOutcomeFromPipeStatus(command, pipeStatus) {
	case evidence.VerificationPassed:
		return tool.ShellVerificationPassed
	case evidence.VerificationFailed:
		return tool.ShellVerificationFailed
	}
	switch {
	// A declared check already passed the whole-command shape gate in preflight,
	// so its exit status is the verdict without the name table having a say.
	case !declared && !evidence.VerificationExitConclusive(command):
		return tool.ShellVerificationInconclusive
	case err != nil:
		return tool.ShellVerificationFailed
	default:
		return tool.ShellVerificationPassed
	}
}

// hostReadsCheckThroughPipeStatus reports whether the pipe-status probe will
// answer this call's check, which is the only thing the maskable-shape block
// protects against. The shape test parses bash, so a shell whose pipelines it
// cannot read fails it and the block still stands.
func hostReadsCheckThroughPipeStatus(args json.RawMessage) bool {
	command := bashCommandFromArgs(args)
	if command == "" || isBackgroundTaskCall(string(args)) {
		return false
	}
	// Both halves are required: the probe answers the check, and nothing outside
	// that pipeline can swallow a mutation's failure on the way there.
	return shellparse.MasksOnlyInsideFinalPipeline(command) &&
		evidence.PipeStatusCanDecideVerification(command)
}

// shellVerificationNotice tells the model at the moment it happens that the
// check it just ran cannot be read. The alternative is finding out at the end
// of the turn, from a gate whose only visible cause is a command it watched
// succeed.
func shellVerificationNotice(output string, execution *tool.ShellExecution) string {
	if execution == nil || execution.Verification != tool.ShellVerificationInconclusive {
		return output
	}
	const notice = "note: this command's exit status is its last stage's, not the check's, so the host cannot read the check's result from it. Re-run the check on its own if it needs to count as verification."
	if strings.TrimSpace(output) == "" {
		return notice
	}
	return output + "\n\n" + notice
}

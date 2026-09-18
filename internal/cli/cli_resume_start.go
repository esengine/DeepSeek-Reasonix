package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"reasonix/internal/agent"
	"reasonix/internal/control"
	"reasonix/internal/i18n"
	"reasonix/internal/session"
)

// headlessResumeTarget resolves --resume/--continue for the non-interactive
// agent path, before any heavy assembly, so --copy and the session lease are
// settled first. --resume takes precedence over --continue, matching the
// Resume call downstream, and accepts file paths, branch IDs, preview text,
// opaque machine session IDs (#7429) and final-format session identities.
// The int is a process exit code: 0 means the (possibly empty) target is
// usable, anything else is the code the command must exit with.
func headlessResumeTarget(resume string, cont, copySession bool) (cliResumeTarget, int) {
	target := cliResumeTarget{}
	if strings.TrimSpace(resume) != "" {
		resolved, err := resolveSessionQuery(resolveCLISessionDir(), strings.TrimSpace(resume))
		if err != nil {
			fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
			return cliResumeTarget{}, 1
		}
		target = resolved
	}
	if target.empty() && cont {
		continued, rc := continueResumeTarget()
		if rc != 0 {
			return cliResumeTarget{}, rc
		}
		target = continued
	}
	return target, copySessionExitCode(copySession, target)
}

// interactiveResumeTarget resolves the same selection for the TUI, where the
// picker sentinel opens the chooser instead of failing. resumeValue must
// already be normalized by normalizedResumeFlag.
func interactiveResumeTarget(resumeValue string, cont, copySession bool) (cliResumeTarget, int) {
	var target cliResumeTarget
	switch {
	case resumeValue == resumePickerSentinel:
		picked, rc := pickSessionToResume()
		if rc != 0 {
			return cliResumeTarget{}, rc
		}
		target = picked
	case resumeValue != "":
		resolved, err := resolveSessionQuery(resolveCLISessionDir(), resumeValue)
		if err != nil {
			fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
			return cliResumeTarget{}, 1
		}
		target = resolved
	case cont:
		continued, rc := continueResumeTarget()
		if rc != 0 {
			return cliResumeTarget{}, rc
		}
		target = continued
	}
	return target, copySessionExitCode(copySession, target)
}

// normalizedResumeFlag maps the bare-flag spellings of --resume onto the
// values the resolver understands. The result also decides the telemetry
// session mode, so it is computed once and shared.
func normalizedResumeFlag(resume string) string {
	value := strings.TrimSpace(resume)
	switch strings.ToLower(value) {
	case "true":
		return resumePickerSentinel
	case "false":
		return ""
	}
	return value
}

// continueResumeTarget implements --continue: sweep the recovery branches this
// workspace left behind, then take the newest conversation. Finding nothing is
// not fatal — the caller falls through to a fresh session — so the miss is
// reported and an empty target returned with a zero exit code.
func continueResumeTarget() (cliResumeTarget, int) {
	sessionDir := resolveCLISessionDir()
	reclaimCLIRecoveryBranches(sessionDir)
	target, ok := newestResumeTarget(sessionDir)
	if !ok {
		fmt.Fprintln(os.Stderr, i18n.M.NoSessionToResumeStartingNew)
		return cliResumeTarget{}, 0
	}
	return target, 0
}

// copySessionExitCode validates --copy against the resolved target: forking
// needs something to fork, and a final-format session has no transcript file
// to copy yet.
func copySessionExitCode(copySession bool, target cliResumeTarget) int {
	switch {
	case !copySession:
		return 0
	case target.empty():
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, "--copy requires --resume or --continue")
	case target.canonical():
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, "--copy does not support final-format sessions yet")
	default:
		return 0
	}
	return 2
}

// commitStartupResume binds the resolved target to ctrl. A final-format
// session whose writer is held elsewhere is taken over only when approve
// agrees, which is the sole difference between --takeover and the TUI prompt.
func commitStartupResume(binding *cliTakeoverBinding, manager *cliTakeoverManager, ctrl *control.Controller,
	resumed *agent.Session, target cliResumeTarget, approve func(error) bool) error {
	err := commitResumedSession(binding, manager, ctrl, resumed, target)
	if err == nil || !target.canonical() || !errors.Is(err, session.ErrWriterOwned) || !approve(err) {
		return err
	}
	return cliStartupCanonicalTakeover(ctrl, manager, target)
}

// flagTakeoverApproval answers for --takeover, which commits before the
// conflict is known.
func flagTakeoverApproval(enabled bool) func(error) bool {
	return func(error) bool { return enabled }
}

// promptTakeoverApproval asks on the terminal; a non-interactive start declines.
func promptTakeoverApproval(err error) bool {
	return isInteractive() && promptSessionTakeover(err)
}

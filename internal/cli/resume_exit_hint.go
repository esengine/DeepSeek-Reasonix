package cli

import (
	"fmt"

	"reasonix/internal/agent"
	"reasonix/internal/control"
	"reasonix/internal/i18n"

	tea "charm.land/bubbletea/v2"
)

// exitWithResumeHint prints a tip for resuming the just-closed conversation
// and returns the normal exit code. Error and Web-handoff paths return before
// reaching it.
func exitWithResumeHint(final tea.Model, ctrl control.SessionAPI) int {
	if hint := resumeExitHint(activeSessionPath(final, ctrl)); hint != "" {
		fmt.Println(hint)
	}
	return 0
}

// activeSessionPath returns the final session path after the TUI exits; a
// /model switch retires the launch controller, so its path wins.
func activeSessionPath(final tea.Model, ctrl control.SessionAPI) string {
	if fm, ok := final.(chatTUI); ok && fm.ctrl != nil {
		return fm.ctrl.SessionPath()
	}
	return ctrl.SessionPath()
}

// resumeExitHint returns a one-line tip for resuming the just-closed
// conversation, or "" when there is no session to resume.
func resumeExitHint(sessionPath string) string {
	if sessionPath == "" {
		return ""
	}
	return fmt.Sprintf(i18n.M.ResumeExitHintFmt, agent.BranchID(sessionPath))
}

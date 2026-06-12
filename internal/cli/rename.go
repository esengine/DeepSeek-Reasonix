package cli

import (
	"fmt"
	"strconv"
	"strings"

	"reasonix/internal/agent"
	"reasonix/internal/i18n"
)

// runRenameCommand handles "/rename": with no argument it shows usage;
// "/rename <new title>" renames the current session;
// "/rename <n> <new title>" renames session #n from the /resume list.
func (m *chatTUI) runRenameCommand(input string) {
	args := tokenizeArgs(input) // args[0] == "/rename"

	if len(args) < 2 {
		m.notice(i18n.M.RenameUsage)
		return
	}

	sessions := recentSessions(m.ctrl.SessionDir())
	title := ""
	targetPath := ""

	// Check if the first arg after /rename is a session index (a number).
	idx, err := strconv.Atoi(args[1])
	if err == nil && len(args) >= 3 {
		// "/rename <n> <new title>"
		if idx < 1 || idx > len(sessions) {
			m.notice(fmt.Sprintf(i18n.M.ResumeBadIndexFmt, len(sessions)))
			return
		}
		targetPath = sessions[idx-1].Path
		title = strings.TrimSpace(strings.TrimPrefix(input, args[0]+" "+args[1]))
	} else {
		// "/rename <new title>" -- rename the current session.
		if m.ctrl.SessionPath() == "" {
			m.notice(i18n.M.RenameNoSession)
			return
		}
		targetPath = m.ctrl.SessionPath()
		title = strings.TrimSpace(strings.TrimPrefix(input, args[0]))
	}

	if title == "" {
		m.notice(i18n.M.RenameUsage)
		return
	}

	if err := agent.RenameSession(targetPath, title); err != nil {
		m.notice("rename: " + err.Error())
		return
	}

	m.notice(fmt.Sprintf(i18n.M.RenameDoneFmt, title))
}

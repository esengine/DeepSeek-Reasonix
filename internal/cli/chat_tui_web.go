package cli

import (
	"os"

	"reasonix/internal/agent"

	tea "charm.land/bubbletea/v2"
)

// webHandoffState is populated by /web before the TUI restores the terminal.
// A fresh session carries only its reserved identity; an existing transcript
// also carries the materialized path so the Web controller can resume it.
type webHandoffState struct {
	launchWebOnExit     bool
	launchWebResumePath string
	launchWebSessionID  string
	launchWebModelRef   string
}

func (m *chatTUI) runHelpOrWebSlash(input, command string) tea.Cmd {
	m.echoLocalCommand(input)
	if command == "/help" {
		m.showHelp()
		return nil
	}
	return m.runWebSlash()
}

func (m *chatTUI) runWebSlash() tea.Cmd {
	if m.runtimeSwitchBusy() {
		m.notice("wait for active work to finish, then retry /web")
		return nil
	}
	if err := m.ctrl.Snapshot(); err != nil {
		m.notice("web: could not save the current session: " + err.Error())
		return nil
	}
	resumePath := m.ctrl.SessionPath()
	if resumePath != "" {
		info, err := os.Stat(resumePath)
		switch {
		case err == nil && !info.Mode().IsRegular():
			m.notice("web: the current session path is not a regular file")
			return nil
		case err == nil:
		case os.IsNotExist(err):
			resumePath = ""
		default:
			m.notice("web: could not inspect the current session: " + err.Error())
			return nil
		}
	}
	m.followSessionLease()
	m.launchWebOnExit = true
	m.launchWebResumePath = resumePath
	m.launchWebSessionID = agent.BranchID(m.ctrl.SessionPath())
	m.launchWebModelRef = m.modelRef
	return shutdownNow
}

func webHandoffArgs(sessionPath, sessionID, modelRef string) []string {
	args := []string{}
	if sessionPath != "" {
		args = append(args, "--resume", sessionPath)
	} else if sessionID != "" {
		args = append(args, "--session-id", sessionID)
	}
	if modelRef != "" {
		args = append(args, "--model", modelRef)
	}
	return args
}

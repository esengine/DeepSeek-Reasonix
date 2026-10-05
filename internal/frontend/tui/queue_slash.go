package tui

import (
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"reasonix/internal/base/i18n"
)

// slashDoneMsg is a local command's answer, shown as one notice.
type slashDoneMsg struct{ level, text string }

// statusReportMsg carries the fresh status a /status report is drawn from.
type statusReportMsg struct {
	s   Status
	git *GitInfo
	err error
}

// takeoverMsg settles /takeover: the screen restarts on the session it bound.
type takeoverMsg struct {
	err  error
	text string
}

// queueSlash answers the commands this screen runs itself, a running turn or
// not: they read or steer the kernel's own state and never start a turn.
func (m *model) queueSlash(display string) (tea.Cmd, bool) {
	fields := strings.Fields(display)
	rest := strings.TrimSpace(strings.TrimPrefix(display, fields[0]))
	var run func() tea.Msg
	switch strings.ToLower(fields[0]) {
	case "/queue":
		run = func() tea.Msg { return queueCommand(m.ctx, m.client, fields[1:]) }
	case "/steer":
		text := m.pastes.expand(rest)
		run = func() tea.Msg { return steerCommand(m.ctx, m.client, text) }
	case "/takeover":
		running := m.tr.Running
		run = func() tea.Msg { return m.takeover(fields[1:], running) }
	case "/rename":
		idx, title, ok := renameByIndexArgs(fields, rest)
		if !ok {
			return nil, false
		}
		run = func() tea.Msg { return m.renameSession(idx, title) }
	case "/status":
		run = func() tea.Msg {
			s, err := m.client.Status(m.ctx)
			msg := statusReportMsg{s: s, err: err}
			if g, gerr := m.client.WorkspaceGit(m.ctx); gerr == nil {
				msg.git = &g
			}
			return msg
		}
	case "/export":
		run = m.exportSession()
	case "/copy":
		n, _ := strconv.Atoi(rest)
		run = m.copyResponse(max(n, 0))
	default:
		return nil, false
	}
	m.history = append(m.history, display)
	m.histAt = len(m.history)
	m.composer.Reset()
	m.tr.AddEcho(display)
	return tea.Batch(m.commit(), run), true
}

// takeover resumes the named session, or the one named by its position in the
// session list. A session another Reasonix process holds is never taken from
// it: the lease stays with its holder and writes here are refused.
func (m *model) takeover(args []string, running bool) tea.Msg {
	if len(args) == 0 {
		return slashDoneMsg{level: "info", text: "takeover: no refused session; run /resume <n> first or pass an index"}
	}
	if running {
		return slashDoneMsg{level: "warn", text: i18n.M.ResumeBusy}
	}
	target := args[0]
	if idx, err := strconv.Atoi(target); err == nil {
		list, err := m.client.Sessions(m.ctx)
		if err != nil {
			return slashDoneMsg{level: "error", text: "takeover: " + err.Error()}
		}
		if idx < 1 || idx > len(list) {
			return slashDoneMsg{level: "info", text: fmt.Sprintf(i18n.M.ResumeBadIndexFmt, len(list))}
		}
		if list[idx-1].Current {
			return slashDoneMsg{level: "info", text: i18n.M.ResumeAlreadyActive}
		}
		target = list[idx-1].Path
	}
	err := m.client.Resume(m.ctx, target)
	return takeoverMsg{err: err, text: i18n.M.TakeoverNoSteal}
}

// renameByIndexArgs recognises "/rename <n> <title>", the form that names a
// saved session by its position; any other shape is the current session's.
func renameByIndexArgs(fields []string, rest string) (idx int, title string, ok bool) {
	if len(fields) < 3 {
		return 0, "", false
	}
	idx, err := strconv.Atoi(fields[1])
	if err != nil {
		return 0, "", false
	}
	return idx, strings.TrimSpace(strings.TrimPrefix(rest, fields[1])), true
}

func (m *model) renameSession(idx int, title string) tea.Msg {
	list, err := m.client.Sessions(m.ctx)
	if err != nil {
		return slashDoneMsg{level: "error", text: "rename: " + err.Error()}
	}
	if idx < 1 || idx > len(list) {
		return slashDoneMsg{level: "info", text: fmt.Sprintf(i18n.M.ResumeBadIndexFmt, len(list))}
	}
	if err := m.client.RenameSession(m.ctx, list[idx-1].Name, title); err != nil {
		return slashDoneMsg{level: "error", text: "rename: " + err.Error()}
	}
	return slashDoneMsg{level: "info", text: fmt.Sprintf(i18n.M.RenameDoneFmt, title)}
}

func (m *model) onSlashDone(msg slashDoneMsg) tea.Cmd {
	m.tr.AddNotice(msg.level, msg.text)
	return m.commit()
}

func (m *model) onTakeover(msg takeoverMsg) tea.Cmd {
	cmd := m.onResumed(resumedMsg{err: msg.err})
	if msg.err != nil {
		return cmd
	}
	return tea.Sequence(cmd, func() tea.Msg { return slashDoneMsg{level: "info", text: msg.text} })
}

func (m *model) onStatusReport(msg statusReportMsg) tea.Cmd {
	if msg.err == nil {
		m.status = msg.s
	}
	if msg.git != nil {
		m.git = *msg.git
	}
	return m.onSlashDone(slashDoneMsg{level: "info", text: m.statusDetails()})
}

// onQueueMsg takes the answers of the commands queueSlash started.
func (m *model) onQueueMsg(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case slashDoneMsg:
		return m.onSlashDone(msg), true
	case statusReportMsg:
		return m.onStatusReport(msg), true
	case takeoverMsg:
		return m.onTakeover(msg), true
	case copyPartsMsg:
		return m.onCopyParts(msg), true
	case copiedMsg:
		return m.onCopiedResponse(msg), true
	}
	return nil, false
}

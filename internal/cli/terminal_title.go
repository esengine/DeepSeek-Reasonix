package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"reasonix/internal/agent"
	"reasonix/internal/config"
)

func sessionWindowTitle(sessionPath string) string {
	if sessionPath == "" {
		return ""
	}
	meta, ok, err := agent.LoadBranchMeta(sessionPath)
	if err != nil || !ok {
		return ""
	}
	return strings.TrimSpace(meta.CustomTitle)
}

func sessionTerminalTitle(sessionPath string) string {
	if sessionPath == "" {
		return ""
	}
	meta, ok, err := agent.LoadBranchMeta(sessionPath)
	if err != nil || !ok {
		return agent.BranchID(sessionPath)
	}
	for _, title := range []string{meta.CustomTitle, meta.TopicTitle, meta.Name} {
		if title = strings.TrimSpace(title); title != "" {
			return title
		}
	}
	if strings.TrimSpace(meta.ID) != "" {
		return meta.ID
	}
	return agent.BranchID(sessionPath)
}

func (m *chatTUI) syncWindowTitle() {
	if m == nil {
		return
	}
	m.windowTitle = m.renderTerminalTitle()
}

func (m chatTUI) renderTerminalTitle() string {
	items := m.terminalTitleItems
	if len(items) == 0 {
		items = config.DefaultTerminalTitleItems()
	}
	items = config.NormalizeTerminalTitleItems(items)
	parts := make([]string, 0, len(items))
	for _, item := range items {
		if part := m.renderTerminalTitleItem(item); part != "" {
			parts = append(parts, part)
		}
	}
	return terminalTitleClean(strings.Join(parts, " | "))
}

func (m chatTUI) renderTerminalTitleItem(item string) string {
	switch item {
	case config.TerminalTitleActivity:
		return m.terminalTitleActivity()
	case config.TerminalTitleSessionTitle:
		if m.ctrl == nil {
			return ""
		}
		return sessionTerminalTitle(m.ctrl.SessionPath())
	case config.TerminalTitleTodoProgress:
		return terminalTitleTodoProgress(m.todoArgs)
	case config.TerminalTitleAppName:
		return "Reasonix"
	case config.TerminalTitleProjectName:
		return m.terminalTitleProjectName()
	case config.TerminalTitleCurrentDir:
		return terminalTitleCurrentDir()
	case config.TerminalTitleRunState:
		return m.terminalTitleRunState()
	case config.TerminalTitleGitBranch:
		return m.terminalTitleGitBranch()
	default:
		return ""
	}
}

func (m chatTUI) terminalTitleActivity() string {
	switch {
	case m.pendingApproval != nil:
		if m.pendingApproval.Tool == planApprovalTool {
			return "Plan approval"
		}
		return "Action required"
	case m.chooser != nil:
		return "Question"
	case m.state == tuiRunning:
		return m.runningWorkingLine(m.cancelRequested(), false)
	case m.modelSwitchPending:
		return "Switching model"
	case m.mcp != nil:
		return "MCP"
	case m.skillPick != nil:
		return "Skills"
	case m.titlePick != nil:
		return "Terminal title"
	case m.rewind != nil:
		return "Rewind"
	case m.resumePick != nil:
		return "Resume"
	default:
		return ""
	}
}

func terminalTitleTodoProgress(args string) string {
	var p struct {
		Todos []todoPanelTodo `json:"todos"`
	}
	if err := json.Unmarshal([]byte(args), &p); err != nil || len(p.Todos) == 0 {
		return ""
	}
	done := 0
	for _, todo := range p.Todos {
		if todo.Status == "completed" {
			done++
		}
	}
	return fmt.Sprintf("Tasks %d/%d", done, len(p.Todos))
}

func (m chatTUI) terminalTitleProjectName() string {
	var root string
	if m.ctrl != nil {
		root = m.ctrl.WorkspaceRoot()
	}
	if strings.TrimSpace(root) == "" {
		root, _ = os.Getwd()
	}
	root = strings.TrimRight(root, string(os.PathSeparator))
	if root == "" {
		return ""
	}
	return filepath.Base(root)
}

func terminalTitleCurrentDir() string {
	cwd, err := os.Getwd()
	if err != nil || strings.TrimSpace(cwd) == "" {
		return ""
	}
	home, err := os.UserHomeDir()
	if err == nil && home != "" {
		if cwd == home {
			return "~"
		}
		prefix := strings.TrimRight(home, string(os.PathSeparator)) + string(os.PathSeparator)
		if strings.HasPrefix(cwd, prefix) {
			return "~" + string(os.PathSeparator) + strings.TrimPrefix(cwd, prefix)
		}
	}
	return cwd
}

func (m chatTUI) terminalTitleRunState() string {
	switch {
	case m.pendingApproval != nil || m.chooser != nil:
		return "Blocked"
	case m.state == tuiRunning && m.cancelRequested():
		return "Stopping"
	case m.state == tuiRunning:
		return "Working"
	default:
		return "Ready"
	}
}

func (m chatTUI) terminalTitleGitBranch() string {
	if strings.TrimSpace(m.gitStatus.Branch) == "" {
		return ""
	}
	if m.gitStatus.Detached {
		return "detached " + strings.TrimSpace(m.gitStatus.Branch)
	}
	return strings.TrimSpace(m.gitStatus.Branch)
}

func terminalTitleClean(s string) string {
	s = ansi.Strip(s)
	s = strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\r' || r == '\t':
			return ' '
		case r < 0x20 || r == 0x7f:
			return -1
		default:
			return r
		}
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if visibleWidth(s) > 160 {
		s = ansi.Truncate(s, 160, "…")
	}
	return strings.TrimSpace(s)
}

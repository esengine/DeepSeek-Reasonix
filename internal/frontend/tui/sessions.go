package tui

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"reasonix/internal/base/i18n"
	"reasonix/internal/frontend/termrender"
)

const pickerRows = 8

// SessionInfo is one saved conversation of this workspace.
type SessionInfo struct {
	Name     string    `json:"name"`
	Path     string    `json:"path"`
	Title    string    `json:"title"`
	Turns    int       `json:"turns"`
	Current  bool      `json:"current"`
	Modified time.Time `json:"modified"`
}

func (c *Client) Sessions(ctx context.Context) ([]SessionInfo, error) {
	var out []SessionInfo
	err := c.do(ctx, http.MethodGet, "/sessions", nil, &out)
	return out, err
}

// Resume binds the runtime to the saved conversation at path.
func (c *Client) Resume(ctx context.Context, path string) error {
	return c.do(ctx, http.MethodPost, "/resume", map[string]string{"path": path}, nil)
}

// sessionPicker chooses a saved conversation to continue. query narrows the
// list as it is typed; sel indexes the narrowed list.
type sessionPicker struct {
	all   []SessionInfo
	query string
	sel   int
}

type (
	sessionsMsg struct {
		list []SessionInfo
		err  error
	}
	resumedMsg struct{ err error }
)

func (m *model) openPicker() tea.Cmd {
	return func() tea.Msg {
		list, err := m.client.Sessions(m.ctx)
		return sessionsMsg{list: list, err: err}
	}
}

// onSessions opens the picker on the first conversation that is not the one
// already open, since that is the one worth switching to.
func (m *model) onSessions(msg sessionsMsg) tea.Cmd {
	among := m.opts.PickAmong
	m.opts.PickAmong = nil
	if msg.err != nil {
		m.tr.AddNotice("error", "sessions: "+msg.err.Error())
		return m.commit()
	}
	if len(among) > 0 {
		msg.list = slices.DeleteFunc(msg.list, func(s SessionInfo) bool { return !slices.Contains(among, s.Path) })
	}
	if len(msg.list) == 0 {
		m.tr.AddNotice("warn", i18n.M.NoSessionToResume)
		return m.commit()
	}
	p := &sessionPicker{all: msg.list}
	for i, s := range msg.list {
		if !s.Current {
			p.sel = i
			break
		}
	}
	m.picker = p
	return nil
}

func sessionLabel(s SessionInfo) string {
	title := s.Title
	if title == "" {
		title = "(no user message yet)"
	}
	return fmt.Sprintf("%d turns · %s", s.Turns, title)
}

func (p *sessionPicker) shown() []SessionInfo {
	if p.query == "" {
		return p.all
	}
	q := strings.ToLower(p.query)
	var out []SessionInfo
	for _, s := range p.all {
		if strings.Contains(strings.ToLower(sessionLabel(s)), q) {
			out = append(out, s)
		}
	}
	return out
}

// pickerYolo reports whether the session's approval mode is YOLO; a picker
// opened in that mode selects a numbered row from a bare digit.
func (m *model) pickerYolo() bool {
	return m.status.ToolApprovalMode == "yolo"
}

// pickerKey takes every key while the picker is open: it is modal, and what
// is typed filters the list rather than reaching the composer.
func (m *model) pickerKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	p := m.picker
	if p == nil {
		return nil, false
	}
	items := p.shown()
	switch k := msg.String(); k {
	case "esc":
		m.picker = nil
	case "up":
		p.sel = max(p.sel-1, 0)
	case "down", "tab":
		p.sel = min(p.sel+1, max(len(items)-1, 0))
	case "enter":
		if p.sel < len(items) {
			return m.pickSession(items[p.sel]), true
		}
	case "backspace":
		if p.query != "" {
			_, size := utf8.DecodeLastRuneInString(p.query)
			p.query, p.sel = p.query[:len(p.query)-size], 0
		}
	default:
		if msg.Text != "" {
			// In YOLO mode a bare digit picks the numbered row directly; a
			// digit still filters once a query is active, so typing a digit
			// to search cannot pick a row.
			if m.pickerYolo() && p.query == "" && len(msg.Text) == 1 && msg.Text[0] >= '1' && msg.Text[0] <= '9' {
				if n := int(msg.Text[0] - '1'); n < len(items) {
					return m.pickSession(items[n]), true
				}
			}
			p.query, p.sel = p.query+msg.Text, 0
		}
	}
	return nil, true
}

func (m *model) pickSession(s SessionInfo) tea.Cmd {
	m.picker = nil
	switch {
	case s.Current:
		m.tr.AddNotice("info", i18n.M.ResumeAlreadyActive)
		return m.commit()
	case m.tr.Running:
		m.tr.AddNotice("warn", i18n.M.ResumeBusy)
		return m.commit()
	}
	return func() tea.Msg { return resumedMsg{err: m.client.Resume(m.ctx, s.Path)} }
}

// onResumed starts the screen over on the conversation it switched to. Full
// screen, the old transcript goes; inline, the terminal keeps what it printed
// and the resumed conversation follows under a title.
func (m *model) onResumed(msg resumedMsg) tea.Cmd {
	if msg.err != nil {
		m.tr.AddNotice("error", "resume: "+msg.err.Error())
		return m.commit()
	}
	m.tr = Transcript{next: m.tr.next}
	m.committed, m.sayShown = map[int]bool{}, map[int]int{}
	m.todos, m.ask, m.menu = nil, nil, nil
	if m.scr != nil {
		m.scr.blocks, m.scr.sel, m.scr.follow = nil, selection{}, true
	}
	title := m.emit(func(int, bool) string { return termrender.Accent("◆ ") + termrender.Bold(i18n.M.ResumedTitle) })
	return tea.Sequence(title, m.fetchHistory(true), tea.Batch(m.fetchStatus(), m.fetchTodos(), m.fetchMeters()))
}

func (m *model) pickerPanel() []string {
	p := m.picker
	items := p.shown()
	width := max(m.width-8, 12)
	lines := []string{termrender.Accent(i18n.M.ResumePickTitle)}
	if p.query != "" {
		lines = append(lines, "  "+termrender.Dim("Search: ")+p.query)
	}
	if len(items) == 0 {
		lines = append(lines, termrender.Dim("  No matches"))
	}
	start := max(min(p.sel-pickerRows/2, len(items)-pickerRows), 0)
	end := min(start+pickerRows, len(items))
	if start > 0 {
		lines = append(lines, termrender.Dim("  ↑ more"))
	}
	for i := start; i < end; i++ {
		s := items[i]
		label := clipVisible(sessionLabel(s), width)
		if s.Current {
			label += " " + termrender.Dim("(active)")
		}
		lines = append(lines, rowLine(i == p.sel, i+1, "", label, s.Current))
		if !s.Modified.IsZero() {
			lines = append(lines, termrender.Dim("     "+s.Modified.Local().Format("2006-01-02 15:04")))
		}
	}
	if end < len(items) {
		lines = append(lines, termrender.Dim("  ↓ more"))
	}
	lines = append(lines, termrender.Dim("Type to filter · "+i18n.M.ResumePickHint))
	return panel(lines, m.width, accentEdge)
}

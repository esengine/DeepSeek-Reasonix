package tui

import (
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"reasonix/internal/base/i18n"
	"reasonix/internal/frontend/termrender"
)

// chooseItem is one row of a quick picker.
type chooseItem struct {
	ID     string
	Label  string
	Desc   string
	Active bool
}

// quickPicker is a modal, searchable single choice: what is typed narrows the
// list and Enter hands the row under the cursor to pick.
type quickPicker struct {
	title string
	items []chooseItem
	query string
	sel   int
	pick  func(chooseItem) tea.Cmd
}

func newQuickPicker(title string, items []chooseItem, pick func(chooseItem) tea.Cmd) *quickPicker {
	p := &quickPicker{title: title, items: items, pick: pick}
	for i, it := range items {
		if it.Active {
			p.sel = i
		}
	}
	return p
}

func (p *quickPicker) shown() []chooseItem {
	q := strings.ToLower(strings.TrimSpace(p.query))
	if q == "" {
		return p.items
	}
	var out []chooseItem
	for _, it := range p.items {
		if strings.Contains(strings.ToLower(it.Label+" "+it.Desc), q) {
			out = append(out, it)
		}
	}
	return out
}

func (m *model) quickKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	p := m.quick
	if p == nil {
		return nil, false
	}
	items := p.shown()
	switch k := msg.String(); k {
	case "esc", "ctrl+c":
		m.quick = nil
	case "up", "ctrl+p":
		p.sel = max(p.sel-1, 0)
	case "down", "tab", "ctrl+n":
		p.sel = min(p.sel+1, max(len(items)-1, 0))
	case "enter":
		if p.sel < len(items) {
			m.quick = nil
			return p.pick(items[p.sel]), true
		}
	case "backspace":
		if p.query != "" {
			_, n := utf8.DecodeLastRuneInString(p.query)
			p.query, p.sel = p.query[:len(p.query)-n], 0
		}
	default:
		if t := typedText(msg); t != "" && !hasControl(t) {
			p.query, p.sel = p.query+t, 0
		}
	}
	return nil, true
}

func (m *model) quickPanel() []string {
	p := m.quick
	items := p.shown()
	p.sel = min(p.sel, max(len(items)-1, 0))
	lines := []string{termrender.Accent(p.title)}
	if p.query != "" {
		lines = append(lines, "  "+termrender.Dim(i18n.M.ResumePickSearch)+p.query)
	}
	if len(items) == 0 {
		lines = append(lines, termrender.Dim("  "+i18n.M.ResumePickNoMatch))
	}
	start := max(min(p.sel-pickerRows/2, len(items)-pickerRows), 0)
	end := min(start+pickerRows, len(items))
	if start > 0 {
		lines = append(lines, termrender.Dim("  ↑ more"))
	}
	width := max(m.width-8, 12)
	for i := start; i < end; i++ {
		it := items[i]
		label := clipVisible(it.Label, width)
		if it.Active {
			label += " " + termrender.Dim("("+i18n.M.SetupActive+")")
		}
		lines = append(lines, rowLine(i == p.sel, i+1, "", label, it.Active))
		if it.Desc != "" {
			lines = append(lines, termrender.Dim("     "+clipVisible(it.Desc, width)))
		}
	}
	if end < len(items) {
		lines = append(lines, termrender.Dim("  ↓ more"))
	}
	lines = append(lines, termrender.Dim(i18n.M.PickHint))
	return panel(lines, m.width, accentEdge)
}

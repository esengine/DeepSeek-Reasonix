package tui

import (
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"reasonix/internal/frontend/termrender"
)

type catalogRow struct {
	name, detail, ref string
	enabled           bool
}

type catalogPicker struct {
	kind     string
	rows     []catalogRow
	models   []ModelChoice
	query    string
	selected int
	saving   bool
	add      *mcpAddState
}

type catalogMsg struct {
	kind    string
	models  []ModelChoice
	choices []CapabilityChoice
	err     error
}

type catalogSavedMsg struct {
	picker *catalogPicker
	err    error
}

func (m *model) catalogSlash(line string) (tea.Cmd, bool) {
	if line == "/mcp add" {
		m.composer.Reset()
		m.menu = nil
		if m.tr.Running {
			m.tr.AddNotice("warn", "wait for the current turn to finish")
			return m.commit(), true
		}
		m.catalog = &catalogPicker{kind: "mcp", add: &mcpAddState{}}
		return nil, true
	}
	kind := strings.TrimPrefix(line, "/")
	if kind == "skill" {
		kind = "skills"
	}
	switch kind {
	case "model", "provider", "skills", "mcp":
	default:
		return nil, false
	}
	m.composer.Reset()
	m.menu = nil
	m.tr.AddEcho(line)
	if m.tr.Running {
		m.tr.AddNotice("warn", "wait for the current turn to finish")
		return m.commit(), true
	}
	return tea.Batch(m.commit(), m.openCatalog(kind)), true
}

func (m *model) openCatalog(kind string) tea.Cmd {
	return func() tea.Msg {
		out := catalogMsg{kind: kind}
		if kind == "model" || kind == "provider" {
			out.models, out.err = m.client.Models(m.ctx)
		} else {
			out.choices, out.err = m.client.Capabilities(m.ctx, kind)
		}
		return out
	}
}

func (m *model) onCatalog(msg catalogMsg) tea.Cmd {
	if msg.err != nil {
		m.tr.AddNotice("error", msg.kind+": "+msg.err.Error())
		return m.commit()
	}
	p := &catalogPicker{kind: msg.kind, models: msg.models}
	seen := map[string]bool{}
	for _, c := range msg.models {
		if c.Answers != "" && c.Answers != "text" {
			continue
		}
		row := catalogRow{name: c.Ref, detail: c.Provider, ref: c.Ref, enabled: c.Active}
		if p.kind == "provider" {
			if seen[c.Provider] {
				continue
			}
			seen[c.Provider] = true
			row.name, row.detail, row.ref = c.Provider, "choose a model", c.Provider
		}
		p.rows = append(p.rows, row)
	}
	for _, c := range msg.choices {
		p.rows = append(p.rows, catalogRow{name: c.Name, detail: c.Description, ref: c.Name, enabled: c.Enabled})
	}
	m.catalog = p
	return nil
}

func (p *catalogPicker) filtered() []catalogRow {
	var rows []catalogRow
	for _, row := range p.rows {
		if strings.Contains(strings.ToLower(row.name+" "+row.detail), strings.ToLower(p.query)) {
			rows = append(rows, row)
		}
	}
	return rows
}

func (m *model) catalogKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	p := m.catalog
	if p == nil {
		return nil, false
	}
	if p.saving {
		return nil, true
	}
	if p.add != nil {
		return m.mcpAddKey(msg), true
	}
	rows := p.filtered()
	switch msg.String() {
	case "esc", "ctrl+c":
		m.catalog = nil
	case "up", "ctrl+p":
		p.selected = max(p.selected-1, 0)
	case "down", "ctrl+n":
		p.selected = min(p.selected+1, max(len(rows)-1, 0))
	case "enter":
		if len(rows) == 0 {
			return nil, true
		}
		row := rows[min(p.selected, len(rows)-1)]
		if p.kind == "provider" {
			p.kind, p.rows, p.query, p.selected = "model", nil, "", 0
			for _, c := range p.models {
				if c.Provider == row.ref && (c.Answers == "" || c.Answers == "text") {
					p.rows = append(p.rows, catalogRow{name: c.Ref, ref: c.Ref, enabled: c.Active})
				}
			}
			return nil, true
		}
		p.saving = true
		return func() tea.Msg {
			var err error
			if p.kind == "model" {
				err = m.client.SelectModel(m.ctx, row.ref)
			} else {
				err = m.client.SetCapabilityEnabled(m.ctx, p.kind, row.ref, !row.enabled)
			}
			return catalogSavedMsg{picker: p, err: err}
		}, true
	case "backspace":
		if p.query != "" {
			_, n := utf8.DecodeLastRuneInString(p.query)
			p.query, p.selected = p.query[:len(p.query)-n], 0
		}
	default:
		if msg.String() == "ctrl+a" && p.kind == "mcp" {
			p.add = &mcpAddState{}
			return nil, true
		}
		if text := typedText(msg); text != "" {
			p.query, p.selected = p.query+text, 0
		}
	}
	return nil, true
}

func (m *model) onCatalogSaved(msg catalogSavedMsg) tea.Cmd {
	if m.catalog != msg.picker {
		return nil
	}
	m.catalog.saving = false
	if msg.err != nil {
		m.tr.AddNotice("error", msg.picker.kind+": "+msg.err.Error())
		return m.commit()
	}
	kind := m.catalog.kind
	m.catalog = nil
	if kind == "model" {
		return m.fetchStatus()
	}
	return m.openCatalog(kind)
}

func (m *model) catalogPanel() []string {
	p := m.catalog
	if p.add != nil {
		return m.mcpAddPanel()
	}
	rows := p.filtered()
	lines := []string{termrender.Accent(p.kind), "  Search: " + p.query}
	start := max(min(p.selected-4, len(rows)-8), 0)
	for i := start; i < min(start+8, len(rows)); i++ {
		row := rows[i]
		label := row.name
		if p.kind == "skills" || p.kind == "mcp" {
			if row.enabled {
				label += " [on]"
			} else {
				label += " [off]"
			}
		}
		lines = append(lines, rowLine(i == p.selected, i+1, "", label, row.enabled))
		if row.detail != "" {
			lines = append(lines, termrender.Dim("     "+oneLine(row.detail, max(m.width-10, 1))))
		}
	}
	if len(rows) == 0 {
		lines = append(lines, termrender.Dim("  No matches"))
	}
	hint := "↑↓ navigate · type to search · Enter select · Esc cancel"
	if p.kind == "skills" || p.kind == "mcp" {
		hint = "↑↓ navigate · type to search · Enter toggle in project · Esc close"
	}
	if p.kind == "mcp" {
		hint += " · Ctrl+A add"
	}
	if p.saving {
		hint = "Saving…"
	}
	lines = append(lines, termrender.Dim(hint))
	return panel(lines, m.width, accentEdge)
}

package cli

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"reasonix/internal/config"
)

type mcpImportPicker struct {
	candidates []config.MCPImportCandidate
	cursor     int
	checked    []bool
}

func newMCPImportPicker(candidates []config.MCPImportCandidate) *mcpImportPicker {
	p := &mcpImportPicker{candidates: candidates, checked: make([]bool, len(candidates))}
	for i, c := range candidates {
		p.checked[i] = c.Recommended
	}
	return p
}

func (m *chatTUI) openMCPImportPicker() {
	candidates, err := config.LoadCCSwitchMCPCandidates()
	if err != nil {
		m.notice("mcp import: " + err.Error())
		return
	}
	if len(candidates) == 0 {
		m.notice("mcp import: no candidates found")
		return
	}
	m.completion = completion{}
	m.mcpImport = newMCPImportPicker(candidates)
}

func (m chatTUI) handleMCPImportKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	p := m.mcpImport
	switch msg.String() {
	case "esc", "ctrl+c":
		m.mcpImport = nil
		return m, nil
	case "up", "k":
		if p.cursor > 0 {
			p.cursor--
		}
	case "down", "j":
		if p.cursor < len(p.candidates)-1 {
			p.cursor++
		}
	case " ", "space":
		p.checked[p.cursor] = !p.checked[p.cursor]
	case "enter":
		var entries []config.PluginEntry
		for i, ok := range p.checked {
			if ok {
				entries = append(entries, p.candidates[i].Entry)
			}
		}
		m.mcpImport = nil
		if len(entries) == 0 {
			m.notice("mcp import: nothing selected")
			return m, nil
		}
		total, added, updated, connected, failed, skipped, err := m.ctrl.ImportMCPEntries(entries)
		if err != nil {
			m.notice("mcp import: " + err.Error())
			return m, nil
		}
		m.host = m.ctrl.Host()
		m.notice(fmt.Sprintf("imported %d selected MCP servers from cc-switch (%d added, %d updated, %d connected, %d failed, %d skipped)", total, added, updated, connected, failed, skipped))
	}
	return m, nil
}

func (m chatTUI) renderMCPImport() string {
	p := m.mcpImport
	if p == nil {
		return ""
	}
	w := max(m.width, 10)
	var b strings.Builder
	b.WriteString(accent("Import MCP from cc-switch") + "\n")
	b.WriteString(dim("Space select · Enter import · Esc cancel") + "\n\n")
	for i, c := range p.candidates {
		box := "[ ]"
		if p.checked[i] {
			box = "[x]"
		}
		mark := " "
		if i == p.cursor {
			mark = "›"
		}
		reasons := strings.Join(c.Reasons, ", ")
		line := fmt.Sprintf("%s %s %-34s %s", mark, box, c.Entry.Name, dim(reasons))
		if i == p.cursor {
			line = reverse(line)
		}
		b.WriteString(line + "\n")
	}
	return choicePanelStyle.Width(w).Render(strings.TrimRight(b.String(), "\n"))
}

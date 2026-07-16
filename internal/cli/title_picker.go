package cli

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"reasonix/internal/config"
)

type titlePicker struct {
	items   []string
	enabled map[string]bool
	sel     int
}

type terminalTitleItemDef struct {
	id   string
	name string
	desc string
}

var terminalTitleItemDefs = []terminalTitleItemDef{
	{config.TerminalTitleActivity, "live-run", "Reasonix live line: thinking seconds, cancel state, output tokens"},
	{config.TerminalTitleSessionTitle, "session", "Title from /rename, branch metadata, or saved session id"},
	{config.TerminalTitleTodoProgress, "todo-write", "todo_write checklist progress pinned above the composer"},
	{config.TerminalTitleMode, "mode", "Ask/Auto/YOLO/Plan/Goal badge from the Reasonix status bar"},
	{config.TerminalTitleModel, "model", "Active provider/model ref selected by /model"},
	{config.TerminalTitleEffort, "effort", "Current /effort reasoning depth when the provider supports it"},
	{config.TerminalTitleContext, "context", "Context usage plus auto-compact headroom"},
	{config.TerminalTitleBalance, "balance", "Provider wallet balance when the model exposes a balance endpoint"},
	{config.TerminalTitleGitBranch, "git", "Repo branch identity from the built-in Reasonix status line"},
	{config.TerminalTitleProjectName, "workspace", "Workspace root folder owned by this controller"},
	{config.TerminalTitleCurrentDir, "cwd", "Process working directory for this CLI session"},
	{config.TerminalTitleAppName, "app", "Literal Reasonix marker"},
	{config.TerminalTitleRunState, "state", "Compact Ready/Working/Stopping/Blocked state for terminals"},
}

func (m *chatTUI) openTitlePicker() {
	items := config.NormalizeTerminalTitleItems(m.terminalTitleItems)
	enabled := map[string]bool{}
	for _, item := range items {
		enabled[item] = true
	}
	m.titlePick = &titlePicker{items: items, enabled: enabled}
}

func (m chatTUI) handleTitlePickerKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	p := m.titlePick
	if p == nil {
		return m, nil
	}
	switch msg.String() {
	case "esc":
		m.titlePick = nil
	case "up", "k":
		if p.sel > 0 {
			p.sel--
		}
	case "down", "j":
		if p.sel < len(terminalTitleItemDefs)-1 {
			p.sel++
		}
	case " ", "space":
		p.toggleSelected()
	case "enter":
		return m.saveTitlePick()
	default:
		if idx, ok := numberKeyIndex(msg.String(), len(terminalTitleItemDefs)); ok {
			p.sel = idx
			p.toggleSelected()
		}
	}
	return m, nil
}

func (p *titlePicker) toggleSelected() {
	if p == nil || p.sel < 0 || p.sel >= len(terminalTitleItemDefs) {
		return
	}
	id := terminalTitleItemDefs[p.sel].id
	p.enabled[id] = !p.enabled[id]
}

func (p *titlePicker) selectedItems() []string {
	if p == nil {
		return config.DefaultTerminalTitleItems()
	}
	out := []string{}
	for _, def := range terminalTitleItemDefs {
		if p.enabled[def.id] {
			out = append(out, def.id)
		}
	}
	return out
}

func (m chatTUI) saveTitlePick() (tea.Model, tea.Cmd) {
	items := m.titlePick.selectedItems()
	if len(items) == 0 {
		m.notice("title: select at least one item")
		return m, nil
	}
	if err := persistTerminalTitleItems(items); err != nil {
		m.notice("title: " + err.Error())
		return m, nil
	}
	m.terminalTitleItems = items
	if m.cfg != nil {
		m.cfg.TerminalTitle.Items = items
	}
	m.titlePick = nil
	m.syncWindowTitle()
	m.notice(fmt.Sprintf("title: saved %s", strings.Join(items, ", ")))
	return m, nil
}

func persistTerminalTitleItems(items []string) error {
	path := config.UserConfigPath()
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("cannot resolve user config path")
	}
	unlock := config.LockUserConfigEdits()
	defer unlock()
	edit := config.LoadForEdit(path)
	if err := edit.SetTerminalTitleItems(items); err != nil {
		return err
	}
	return edit.SaveTo(path)
}

func (m chatTUI) renderTitlePicker() string {
	p := m.titlePick
	if p == nil {
		return ""
	}
	w := max(viewWidth(m.width), 40)
	return managerContentPanelStyle(w).Render(m.renderTitlePickerBody(w))
}

func (m chatTUI) renderTitlePickerBody(width int) string {
	p := m.titlePick
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", viewHeader("Reasonix Terminal Title"))
	fmt.Fprintf(&b, "%s\n\n", viewMeta("Pick Reasonix session/status fields to mirror into the terminal tab title."))
	for i, def := range terminalTitleItemDefs {
		b.WriteString(renderTitlePickerRow(i, i == p.sel, p.enabled[def.id], def, width))
		b.WriteByte('\n')
	}
	preview := m
	preview.terminalTitleItems = p.selectedItems()
	preview.titlePick = nil
	previewTitle := preview.renderTerminalTitle()
	if previewTitle == "" {
		previewTitle = "(waiting for Reasonix session or run metadata)"
	}
	b.WriteByte('\n')
	b.WriteString(viewSubhead("Preview") + "\n")
	b.WriteString("  " + viewCompactText(previewTitle, viewBudget(width, 2)) + "\n")
	return strings.TrimRight(b.String(), "\n")
}

func renderTitlePickerRow(_ int, selected, enabled bool, def terminalTitleItemDef, width int) string {
	prefix := "    "
	if selected {
		prefix = accent("  › ")
	}
	check := "[ ]"
	if enabled {
		check = "[x]"
	}
	name := fmt.Sprintf("%-12s", def.name)
	used := 4 + visibleWidth(check) + 1 + visibleWidth(name) + 2
	desc := viewMeta(viewCompactText(def.desc, viewBudget(width, used)))
	line := fmt.Sprintf("%s%s %s  %s", prefix, check, name, desc)
	if selected {
		return accent(line)
	}
	return line
}

func (m chatTUI) titlePickerFooterHint() string {
	if m.titlePick == nil {
		return ""
	}
	return "↑/↓ move · Space toggle · Enter save · Esc close"
}

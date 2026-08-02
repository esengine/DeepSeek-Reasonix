package cli

import (
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
)

const quickPickerMaxVisible = 8

type quickPickerKind string

const (
	quickPickerModel         quickPickerKind = "model"
	quickPickerProvider      quickPickerKind = "provider"
	quickPickerProviderModel quickPickerKind = "provider-model"
	quickPickerResume        quickPickerKind = "resume"
)

type quickPickerItem struct {
	ID          string
	Label       string
	Description string
	Status      string
}

// quickPicker is the shared single-choice overlay used by commands that need
// Claude Code-style searchable lists inside Bubble Tea's event loop.
type quickPicker struct {
	kind     quickPickerKind
	title    string
	hint     string
	items    []quickPickerItem
	query    string
	selected int // index in filteredItems, not items
}

type quickPickerResult struct {
	choice    *quickPickerItem
	cancelled bool
}

func (p *quickPicker) filteredItems() []quickPickerItem {
	if p == nil || strings.TrimSpace(p.query) == "" {
		if p == nil {
			return nil
		}
		return p.items
	}
	query := strings.ToLower(strings.TrimSpace(p.query))
	out := make([]quickPickerItem, 0, len(p.items))
	for _, item := range p.items {
		haystack := strings.ToLower(item.Label + " " + item.Description + " " + item.Status)
		if strings.Contains(haystack, query) {
			out = append(out, item)
		}
	}
	return out
}

func (p *quickPicker) handleKey(msg tea.KeyPressMsg) quickPickerResult {
	if p == nil {
		return quickPickerResult{}
	}
	items := p.filteredItems()
	key := msg.String()
	// Match Claude Code's searchable menus: bare j/k navigate until a search
	// starts, then become ordinary query characters. Arrows and Ctrl+P/N always
	// remain available for navigation.
	if p.query == "" {
		switch key {
		case "k":
			key = "up"
		case "j":
			key = "down"
		}
	}
	switch key {
	case "esc":
		return quickPickerResult{cancelled: true}
	case "up", "ctrl+p":
		if p.selected > 0 {
			p.selected--
		}
	case "down", "ctrl+n":
		if p.selected < len(items)-1 {
			p.selected++
		}
	case "enter":
		if p.selected >= 0 && p.selected < len(items) {
			choice := items[p.selected]
			return quickPickerResult{choice: &choice}
		}
	case "backspace":
		if p.query != "" {
			_, n := utf8.DecodeLastRuneInString(p.query)
			p.query = p.query[:len(p.query)-n]
			p.selected = 0
		}
	default:
		text := msg.Text
		if text == "" {
			s := msg.String()
			if len(s) == 1 && s[0] >= 32 && s[0] < 127 {
				text = s
			}
		}
		if text != "" {
			p.query += text
			p.selected = 0
		}
	}
	return quickPickerResult{}
}

func (p *quickPicker) render(width int) string {
	if p == nil {
		return ""
	}
	items := p.filteredItems()
	if p.selected >= len(items) {
		p.selected = max(len(items)-1, 0)
	}
	if len(items) == 0 {
		// No matches: keep the sheet frame with a hint instead of an empty box.
		return renderPickerSheet(p.title, p.query, nil, 0, quickPickerMaxVisible, width, "No matches")
	}
	rows := make([]pickerSheetRow, len(items))
	for i, item := range items {
		hint := ""
		if item.Description != "" {
			hint = item.Description
		}
		if item.Status != "" {
			if hint != "" {
				hint += " · "
			}
			hint += item.Status
		}
		rows[i] = pickerSheetRow{label: item.Label, hint: hint}
	}
	hint := p.hint
	if hint == "" {
		hint = "Type to filter · ↑/↓ navigate · Enter select · Esc cancel"
	}
	return renderPickerSheet(p.title, p.query, rows, p.selected, quickPickerMaxVisible, width, hint)
}

func quickPickerWindow(total, selected int) (int, int) {
	if total <= quickPickerMaxVisible {
		return 0, total
	}
	start := selected - quickPickerMaxVisible/2
	if start < 0 {
		start = 0
	}
	if maxStart := total - quickPickerMaxVisible; start > maxStart {
		start = maxStart
	}
	return start, start + quickPickerMaxVisible
}

// quickPickerItemRows returns the number of item rows the quick picker's
// sheet shows: at most quickPickerMaxVisible, never more than the list.
func (m chatTUI) quickPickerItemRows() int {
	p := m.quickPick
	if p == nil {
		return 0
	}
	items := p.filteredItems()
	if len(items) < quickPickerMaxVisible {
		return len(items)
	}
	return quickPickerMaxVisible
}

// quickPickerHeaderRows counts the title + search lines above the item rows.
func (m chatTUI) quickPickerHeaderRows() int {
	if m.quickPick == nil {
		return 0
	}
	h := 1
	if m.quickPick.query != "" {
		h++
	}
	return h
}

// inQuickPickerScrollbar reports whether (x, y) is on the quick picker's
// scrollbar column (only when the filtered list overflows the sheet).
func (m chatTUI) inQuickPickerScrollbar(x, y int) bool {
	p := m.quickPick
	if p == nil {
		return false
	}
	items := p.filteredItems()
	rows := m.quickPickerItemRows()
	if len(items) <= rows || rows <= 0 {
		return false
	}
	top, _ := m.bottomSheetBounds(rows + m.quickPickerHeaderRows() + 1) // + hint footer
	itemsTop := top + m.quickPickerHeaderRows()
	if y < itemsTop || y >= itemsTop+rows {
		return false
	}
	return x == m.contentWidth()-1
}

// quickPickerScrollbarGrabRowOffset returns where inside the thumb a click
// grabbed, so the thumb doesn't jump to the cursor.
func (m chatTUI) quickPickerScrollbarGrabRowOffset(row int) int {
	p := m.quickPick
	if p == nil {
		return 0
	}
	items := p.filteredItems()
	rows := m.quickPickerItemRows()
	top, _ := m.bottomSheetBounds(rows + m.quickPickerHeaderRows() + 1)
	rowInMenu := row - (top + m.quickPickerHeaderRows())
	if rowInMenu < 0 {
		rowInMenu = 0
	}
	return sheetScrollbarGrabRowOffset(rowInMenu, rows, len(items), completionWindowStart(p.selected, len(items), rows))
}

// dragQuickPickerScrollbar maps a drag row to a list position and moves the
// selection there.
func (m *chatTUI) dragQuickPickerScrollbar(row int) {
	p := m.quickPick
	if p == nil || m.sheetScrollbar == nil {
		return
	}
	items := p.filteredItems()
	rows := m.quickPickerItemRows()
	if len(items) <= rows {
		return
	}
	top, _ := m.bottomSheetBounds(rows + m.quickPickerHeaderRows() + 1)
	rowInMenu := row - (top + m.quickPickerHeaderRows())
	if rowInMenu < 0 {
		rowInMenu = 0
	}
	if rowInMenu >= rows {
		rowInMenu = rows - 1
	}
	p.selected = sheetDragSel(rows, len(items), rowInMenu, m.sheetScrollbar.grab)
}

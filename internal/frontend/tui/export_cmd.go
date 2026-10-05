package tui

import (
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"reasonix/internal/base/i18n"
	"reasonix/internal/frontend/termrender"
)

// copyParts is the assistant text since the last thing the person said,
// oldest first, without the model's empty placeholders.
func copyParts(msgs []HistoryMessage) []string {
	start := 0
	for i, msg := range slices.Backward(msgs) {
		if msg.Role == "user" && !msg.HostAuthored {
			start = i + 1
			break
		}
	}
	var parts []string
	for _, msg := range msgs[start:] {
		if msg.Role != "assistant" {
			continue
		}
		if c := strings.TrimSpace(msg.Content); c != "" && c != "..." && c != "…" {
			parts = append(parts, c)
		}
	}
	return parts
}

// exportSession has the kernel save the conversation in its workspace.
func (m *model) exportSession() tea.Cmd {
	return func() tea.Msg {
		path, n, err := m.client.ExportConversation(m.ctx)
		switch {
		case err != nil:
			return slashDoneMsg{level: "error", text: "export: " + err.Error()}
		case n == 0:
			return slashDoneMsg{level: "info", text: i18n.M.SlashExportEmpty}
		}
		return slashDoneMsg{level: "info", text: fmt.Sprintf(i18n.M.SlashExportDoneFmt, path)}
	}
}

type (
	copyPartsMsg struct {
		parts  []string
		direct int
		err    error
	}
	copiedMsg struct{ termrender.ClipboardCopyMsg }
)

// copyPicker lists the assistant messages of the latest turn, newest first.
type copyPicker struct {
	parts []string
	sel   int
}

// copyResponse copies the nth-newest assistant message when n is given, or
// opens the picker when it is not.
func (m *model) copyResponse(n int) tea.Cmd {
	return func() tea.Msg {
		msgs, err := m.client.History(m.ctx)
		if err != nil {
			return copyPartsMsg{err: err}
		}
		parts := copyParts(msgs)
		for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
			parts[i], parts[j] = parts[j], parts[i]
		}
		return copyPartsMsg{parts: parts, direct: n}
	}
}

func (m *model) onCopyParts(msg copyPartsMsg) tea.Cmd {
	switch {
	case msg.err != nil:
		m.tr.AddNotice("error", "copy: "+msg.err.Error())
		return m.commit()
	case len(msg.parts) == 0 || msg.direct > len(msg.parts):
		m.tr.AddNotice("info", i18n.M.SlashCopyEmpty)
		return m.commit()
	case msg.direct > 0:
		return m.copyText(msg.parts[msg.direct-1])
	}
	m.copying = &copyPicker{parts: msg.parts}
	return nil
}

func (m *model) copyText(text string) tea.Cmd {
	clip := termrender.CopyToClipboard(text)
	return func() tea.Msg { return copiedMsg{clip().(termrender.ClipboardCopyMsg)} }
}

func (m *model) onCopiedResponse(msg copiedMsg) tea.Cmd {
	if msg.Err != nil {
		m.tr.AddNotice("error", "copy: "+msg.Err.Error())
		return m.commit()
	}
	m.tr.AddNotice("info", i18n.M.SlashCopyDone)
	if msg.OSC52 {
		return tea.Batch(m.commit(), tea.SetClipboard(msg.Text))
	}
	return m.commit()
}

// copyKey takes every key while the picker is open.
func (m *model) copyKey(k string) (tea.Cmd, bool) {
	p := m.copying
	if p == nil {
		return nil, false
	}
	switch k {
	case "up", "k":
		p.sel = max(p.sel-1, 0)
	case "down", "j":
		p.sel = min(p.sel+1, len(p.parts)-1)
	case "esc":
		m.copying = nil
	case "enter":
		m.copying = nil
		return m.copyText(p.parts[p.sel]), true
	}
	return nil, true
}

func (m *model) copyPanel() []string {
	p := m.copying
	lines := []string{termrender.Accent(i18n.M.SlashCopyListHeader)}
	width := max(m.width-8, 12)
	for i, part := range p.parts {
		lines = append(lines, rowLine(i == p.sel, i+1, "", clipVisible(firstLine(part), width), false))
	}
	lines = append(lines, termrender.Dim("↑/↓ navigate · Enter copy · Esc cancel"))
	return panel(lines, m.width, accentEdge)
}

// firstLine is the first non-empty line of s, cut to 80 runes.
func firstLine(s string) string {
	for line := range strings.SplitSeq(s, "\n") {
		if t := strings.TrimSpace(line); t != "" {
			if r := []rune(t); len(r) > 80 {
				return string(r[:77]) + "..."
			}
			return t
		}
	}
	return "..."
}

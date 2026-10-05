package tui

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"reasonix/internal/base/i18n"
)

// A paste this large stands in the composer as one token and goes to the
// model whole, with the thresholds 1.x folds at.
const (
	pasteFoldChars = 1000
	pasteFoldLines = 5
)

var imageToken = regexp.MustCompile(`\[image #(\d+)\]`)

type pasteBlock struct{ label, text string }

type pasteStore struct {
	next   int
	blocks []pasteBlock
	images []string
}

// image returns the composer's token for a pasted image; sending turns it
// back into the reference the kernel stored the image under.
func (p *pasteStore) image(ref string) string {
	p.images = append(p.images, ref)
	return fmt.Sprintf("[image #%d]", len(p.images))
}

// pastedLines counts rows the way a terminal ends them: LF, CRLF or a bare CR.
func pastedLines(text string) int {
	if text == "" {
		return 0
	}
	return strings.Count(text, "\n") + 1
}

func foldedPasteLabel(id, lines int) string {
	return fmt.Sprintf("[Pasted text #%d · %d lines]", id, lines)
}

// fold returns what the composer shows for a paste: the text itself, or a
// labelled token for it when it would bury the line being written.
func (p *pasteStore) fold(text string) string {
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	lines := pastedLines(text)
	if len([]rune(text)) < pasteFoldChars && lines < pasteFoldLines {
		return text
	}
	p.next++
	label := foldedPasteLabel(p.next, lines)
	p.blocks = append(p.blocks, pasteBlock{label: label, text: text})
	return label + " "
}

// seed moves the numbering past every label the session already carries, so a
// resumed conversation never gets a second paste with the same label.
func (p *pasteStore) seed(history []HistoryMessage) {
	for _, h := range history {
		for _, m := range foldedLabel.FindAllStringSubmatch(h.Content, -1) {
			if n, err := strconv.Atoi(m[1]); err == nil && n > p.next {
				p.next = n
			}
		}
	}
}

var foldedLabel = regexp.MustCompile(`\[Pasted text #(\d+) · \d+ lines\]`)

// expand replaces each label with the block 1.x sends: the label, then the
// text between Begin and End markers that name it.
func (p *pasteStore) expand(s string) string {
	s = imageToken.ReplaceAllStringFunc(s, func(tok string) string {
		n, _ := strconv.Atoi(imageToken.FindStringSubmatch(tok)[1])
		if n >= 1 && n <= len(p.images) {
			return p.images[n-1]
		}
		return tok
	})
	for _, b := range p.blocks {
		if strings.Contains(s, b.label) {
			s = strings.ReplaceAll(s, b.label, fmt.Sprintf("%s\n\n--- Begin %s ---\n%s\n--- End %s ---", b.label, b.label, b.text, b.label))
		}
	}
	return s
}

// insertPaste puts pasted text in the composer, folded unless a panel is
// taking the keys: what is typed into one is an answer, not a message.
func (m *model) insertPaste(text string) {
	if m.setup != nil {
		m.pasteIntoSetup(text)
		return
	}
	if m.tr.OpenPrompt() != nil || m.picker != nil || m.skills != nil || m.quick != nil || m.mcp != nil || m.rewind != nil || m.copying != nil || m.clearing != nil {
		m.composer.InsertString(text)
		return
	}
	m.composer.InsertString(m.pastes.fold(text))
}

func (m *model) onKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if cmd, handled := m.screenKey(msg); handled {
		return m, cmd
	}
	if cmd, handled := m.shortcutKey(msg.String()); handled {
		return m, cmd
	}
	if cmd, handled := m.menuKey(msg.String()); handled {
		return m, cmd
	}
	if cmd, handled := m.viKey(msg); handled {
		return m, cmd
	}
	empty := m.composer.Value() == ""
	switch msg.String() {
	case "tab":
		return m, m.fetchCompletion()
	case "enter":
		return m, m.send(false)
	case "ctrl+s":
		return m, m.send(true)
	case "esc":
		return m, m.escape(empty)
	case "ctrl+c":
		switch {
		case m.tr.Running:
			m.cancelling = true
			return m, m.call("cancel", m.client.Cancel)
		case !empty:
			m.composer.Reset()
			m.shell = false
			return m, nil
		case time.Since(m.quitArmedAt) < quitArmWindow:
			return m, tea.Quit
		}
		m.quitArmedAt = time.Now()
		return m, nil
	case "ctrl+d":
		if empty && !m.tr.Running {
			return m, tea.Quit
		}

	case "backspace":
		if m.shell && empty {
			m.shell = false
			return m, nil
		}
	case "!":
		if empty && !m.shell {
			m.shell = true
			return m, nil
		}
	case "up", "down":
		if m.composer.LineCount() <= 1 && m.recall(msg.String() == "up") {
			return m, nil
		}
	}
	before := m.composer.Value()
	var cmd tea.Cmd
	m.composer, cmd = m.composer.Update(msg)
	if m.composer.Value() != before {
		return m, tea.Batch(cmd, m.refreshMenu())
	}
	return m, cmd
}

// shortcutKey takes the keys that act without touching the composer: the
// approval modes and the clipboard.
func (m *model) shortcutKey(k string) (tea.Cmd, bool) {
	switch {
	case k == "shift+tab":
		return m.cycleMode(), true
	case k == "ctrl+y":
		return m.toggleYolo(), true
	case imagePasteKey(k):
		return m.pasteClipboard(), true
	case k == "shift+insert":
		return m.pasteClipboardText(), true
	}
	return nil, false
}

// screenKey takes what a key means before it reaches the composer: copying a
// selection, moving the transcript, answering an open panel.
func (m *model) screenKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if cmd, handled := m.selectionKey(msg.String()); handled {
		return cmd, true
	}
	if m.scrollKey(msg.String()) {
		return nil, true
	}
	if cmd, handled := m.setupKey(msg); handled {
		return cmd, true
	}
	if cmd, handled := m.skillsKey(msg); handled {
		return cmd, true
	}
	if cmd, handled := m.quickKey(msg); handled {
		return cmd, true
	}
	if cmd, handled := m.mcpKey(msg); handled {
		return cmd, true
	}
	if cmd, handled := m.pickerKey(msg); handled {
		return cmd, true
	}
	if cmd, handled := m.rewindKey(msg.String()); handled {
		return cmd, true
	}
	if cmd, handled := m.copyKey(msg.String()); handled {
		return cmd, true
	}
	if cmd, handled := m.clearKey(msg.String()); handled {
		return cmd, true
	}
	return m.promptKey(msg)
}

// selectionKey ends a transcript selection on any key; the copy keys copy
// it first, since the terminal never sees a highlight the app drew.
func (m *model) selectionKey(k string) (tea.Cmd, bool) {
	if m.scr == nil || !m.scr.sel.active {
		return nil, false
	}
	copyIt := !m.scr.sel.empty() && (k == "ctrl+c" || k == "super+c" || k == "ctrl+insert")
	var cmd tea.Cmd
	if copyIt {
		cmd = m.copySelection()
	}
	m.scr.sel = selection{}
	return cmd, copyIt
}

// promptKey gives an open panel the keys it owns. The composer is hidden
// behind the panel unless a typed answer has it, so the rest go nowhere but
// ctrl+c, which still stops the turn.
func (m *model) promptKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	open := m.tr.OpenPrompt()
	if open == nil {
		return nil, false
	}
	answer := m.answerApproval
	if open.Kind == ItemAsk {
		answer = m.answerAsk
	}
	if cmd, handled := answer(open, msg.String()); handled {
		return cmd, true
	}
	if open.Kind == ItemAsk && m.ask != nil && m.ask.entering() && msg.String() != "ctrl+c" {
		var cmd tea.Cmd
		m.composer, cmd = m.composer.Update(msg)
		return cmd, true
	}
	return nil, msg.String() != "ctrl+c"
}

// send hands the composer's text to the kernel. Idle, it starts a turn (or
// runs the `!` command); while a turn runs, it queues: steer lands at the
// turn's next tool boundary, a follow-up once the turn is done.
func (m *model) send(steer bool) tea.Cmd {
	display := strings.TrimSpace(m.composer.Value())
	if display == "" {
		return nil
	}
	if cmd, ok := m.queueSlash(display); ok {
		return cmd
	}
	name, _, _ := strings.Cut(display, " ")
	switch {
	case isHelp(name):
		m.composer.Reset()
		m.tr.AddEcho(display)
		return tea.Batch(m.commit(), m.showHelp())
	case name == "/mouse" && m.scr != nil:
		m.composer.Reset()
		return m.toggleMouse()
	case name == "/resume":
		m.composer.Reset()
		return m.openPicker()
	case display == "/rewind" && !m.tr.Running:
		m.composer.Reset()
		m.tr.AddEcho(display)
		return tea.Batch(m.commit(), m.openRewind())
	case name == "/clear" && !m.tr.Running:
		m.composer.Reset()
		m.tr.AddEcho(display)
		return tea.Batch(m.commit(), m.askClear())
	case isSetup(name) && !m.tr.Running:
		m.composer.Reset()
		m.tr.AddEcho(display)
		return tea.Batch(m.commit(), m.openSetup())
	case name == "/version":
		m.composer.Reset()
		version := m.opts.Version
		if version == "" {
			version = "dev"
		}
		m.tr.AddNotice("info", "reasonix "+version)
		return m.commit()
	}
	if name == "/paste-image" {
		m.composer.Reset()
		return m.pasteClipboard()
	}
	if display == "/skills" || display == "/skill" {
		m.composer.Reset()
		m.tr.AddEcho(display)
		return tea.Batch(m.commit(), m.openSkills())
	}
	if cmd, ok := m.modelSlash(display); ok {
		return cmd
	}
	if display == "/mcp" {
		m.composer.Reset()
		m.tr.AddEcho(display)
		return tea.Batch(m.commit(), m.openMCP())
	}
	if cmd, ok := m.miscSlash(display); ok {
		return cmd
	}
	text := m.pastes.expand(display)
	m.history = append(m.history, display)
	m.histAt = len(m.history)
	m.composer.Reset()
	m.viCmd = false
	if m.shell {
		m.shell = false
		if m.tr.Running {
			m.tr.AddNotice("warn", i18n.M.ShellWaitsForTurn)
			return m.commit()
		}
		m.tr.AddEcho("! " + display)
		return tea.Batch(m.commit(), m.call("send", func(ctx context.Context) error { return m.client.RunShell(ctx, text) }))
	}
	if m.tr.Running {
		row := m.tr.AddQueued(display, steer)
		return func() tea.Msg {
			id, err := m.client.Queue(m.ctx, text, steer)
			return queuedMsg{row: row, itemID: id, err: err}
		}
	}
	m.tr.AddUser(display)
	return tea.Batch(m.commit(), func() tea.Msg {
		return sentMsg{display: display, err: m.client.Submit(m.ctx, text)}
	})
}

// escape backs out of the most specific thing in progress: the running turn,
// then what is typed, then shell mode. On an empty idle composer a second Esc
// soon after the first opens the rewind picker.
func (m *model) escape(empty bool) tea.Cmd {
	switch {
	case m.tr.Running:
		m.cancelling = true
		return m.call("cancel", m.client.Cancel)
	case !empty:
		m.composer.Reset()
		return nil
	case m.shell:
		m.shell = false
		return nil
	case time.Since(m.lastEsc) < escArmWindow:
		m.lastEsc = time.Time{}
		return m.openRewind()
	}
	m.lastEsc = time.Now()
	return nil
}

// recall walks the composer through what was sent in this session. The
// draft the walk starts from is set aside and comes back at its end.
func (m *model) recall(back bool) bool {
	live := m.histAt == len(m.history)
	if len(m.history) == 0 || (live && !back) {
		return false
	}
	if live {
		m.draft = m.composer.Value()
	}
	if back {
		m.histAt = max(m.histAt-1, 0)
	} else {
		m.histAt++
	}
	if m.histAt == len(m.history) {
		m.composer.SetValue(m.draft)
		m.draft = ""
	} else {
		m.composer.SetValue(m.history[m.histAt])
	}
	return true
}

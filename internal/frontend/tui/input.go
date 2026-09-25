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

// A paste this large stands in the composer as one token, the way the reader
// would describe it, and goes to the model whole.
const (
	pasteFoldChars = 800
	pasteFoldLines = 3
)

var (
	pasteToken = regexp.MustCompile(`\[Pasted text #(\d+) \+\d+ lines\]`)
	imageToken = regexp.MustCompile(`\[image #(\d+)\]`)
)

type pasteStore struct {
	next   int
	texts  map[int]string
	images []string
}

// image returns the composer's token for a pasted image; sending turns it
// back into the reference the kernel stored the image under.
func (p *pasteStore) image(ref string) string {
	p.images = append(p.images, ref)
	return fmt.Sprintf("[image #%d]", len(p.images))
}

// fold returns what the composer shows for a paste: the text itself, or a
// token for it when it would bury the line being written.
func (p *pasteStore) fold(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	lines := strings.Count(text, "\n") + 1
	if len(text) < pasteFoldChars && lines <= pasteFoldLines {
		return text
	}
	if p.texts == nil {
		p.texts = map[int]string{}
	}
	p.next++
	p.texts[p.next] = text
	return fmt.Sprintf("[Pasted text #%d +%d lines]", p.next, lines)
}

// expand replaces each paste token with the text it stands for.
func (p *pasteStore) expand(s string) string {
	s = imageToken.ReplaceAllStringFunc(s, func(tok string) string {
		n, _ := strconv.Atoi(imageToken.FindStringSubmatch(tok)[1])
		if n >= 1 && n <= len(p.images) {
			return p.images[n-1]
		}
		return tok
	})
	return pasteToken.ReplaceAllStringFunc(s, func(tok string) string {
		n, _ := strconv.Atoi(pasteToken.FindStringSubmatch(tok)[1])
		if text, ok := p.texts[n]; ok {
			return text
		}
		return tok
	})
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
	empty := m.composer.Value() == ""
	switch msg.String() {
	case "tab":
		return m, m.fetchCompletion()
	case "enter":
		return m, m.send(false)
	case "ctrl+s":
		return m, m.send(true)
	case "esc":
		if m.tr.Running {
			m.cancelling = true
			return m, m.call("cancel", m.client.Cancel)
		}
		if m.shell && empty {
			m.shell = false
		}
		return m, nil
	case "ctrl+c":
		switch {
		case m.tr.Running:
			m.cancelling = true
			return m, m.call("cancel", m.client.Cancel)
		case !empty:
			m.composer.Reset()
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
// approval modes and the clipboard's image.
func (m *model) shortcutKey(k string) (tea.Cmd, bool) {
	switch {
	case k == "shift+tab":
		return m.cycleMode(), true
	case k == "ctrl+y":
		return m.toggleYolo(), true
	case imagePasteKey(k):
		return m.pasteClipboard(), true
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
	if cmd, handled := m.pickerKey(msg); handled {
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
	if open.Kind == ItemAsk && m.ask != nil && m.ask.typing {
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
	switch {
	case display == "/mouse" && m.scr != nil:
		m.composer.Reset()
		return m.toggleMouse()
	case display == "/resume":
		m.composer.Reset()
		return m.openPicker()
	}
	text := m.pastes.expand(display)
	m.history = append(m.history, display)
	m.histAt = len(m.history)
	m.composer.Reset()
	if m.shell {
		m.shell = false
		display, text = "! "+display, "!"+text
		if m.tr.Running {
			m.tr.AddNotice("warn", i18n.M.ShellWaitsForTurn)
			return m.commit()
		}
	}
	if m.tr.Running {
		row := m.tr.AddQueued(display, steer)
		return func() tea.Msg {
			id, err := m.client.Queue(m.ctx, text, steer)
			return queuedMsg{row: row, itemID: id, err: err}
		}
	}
	m.tr.AddUser(display)
	return tea.Batch(m.commit(), m.call("send", func(ctx context.Context) error { return m.client.Submit(ctx, text) }))
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

// cycleMode steps ask → auto → plan → ask, the order the footer names them.
// Plan is its own switch on the kernel, so entering it leaves the approval
// mode where it was and leaving it returns to ask.
func (m *model) cycleMode() tea.Cmd {
	s := &m.status
	var step func(context.Context) error
	switch {
	case s.Plan:
		s.Plan, s.ToolApprovalMode = false, "ask"
		step = func(ctx context.Context) error {
			if err := m.client.SetPlan(ctx, false); err != nil {
				return err
			}
			return m.client.SetApprovalMode(ctx, "ask")
		}
	case s.ToolApprovalMode == "auto":
		s.Plan = true
		step = func(ctx context.Context) error { return m.client.SetPlan(ctx, true) }
	default:
		s.ToolApprovalMode = "auto"
		step = func(ctx context.Context) error { return m.client.SetApprovalMode(ctx, "auto") }
	}
	return tea.Sequence(m.call("mode", step), m.fetchStatus())
}

// toggleYolo flips between skipping approvals and asking for them.
func (m *model) toggleYolo() tea.Cmd {
	next := "yolo"
	if m.status.ToolApprovalMode == "yolo" {
		next = "ask"
	}
	m.status.ToolApprovalMode = next
	return tea.Sequence(m.call("mode", func(ctx context.Context) error { return m.client.SetApprovalMode(ctx, next) }), m.fetchStatus())
}

package tui

import (
	"context"
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"reasonix/internal/base/i18n"
	"reasonix/internal/frontend/termrender"
)

// askState walks an open question panel. tab is the question in view; with
// more than one question and auto-submit off, one tab past the last reviews
// the answers. Each question's rows are its options, then a typed answer, then
// declining. A single-choice pick may carry a note, sent after it as a second
// selection.
type askState struct {
	item          int
	tab           int
	cursor        int
	picks         [][]string
	custom        []string
	notes         []string
	entry         askEntry
	urlSubmission urlSubmission
}

// askEntry is what the composer is taking while the panel is open.
type askEntry int

const (
	entryNone   askEntry = iota
	entryAnswer          // an answer no option offered
	entryNote            // a note under this question's single-choice pick
)

func (st *askState) entering() bool { return st.entry != entryNone }

func (m *model) openAsk(it *Item) *askState {
	if m.ask == nil || m.ask.item != it.ID {
		n := len(it.Ask.Questions)
		m.ask = &askState{item: it.ID, picks: make([][]string, n), custom: make([]string, n), notes: make([]string, n)}
		if it.Ask.Origin != nil && it.Ask.Origin.URL != "" {
			m.ask.cursor = 3
		}
		for i, q := range it.Ask.Questions {
			m.ask.picks[i] = slices.Clone(q.Default)
		}
	}
	return m.ask
}

func (st *askState) onSubmitTab(n int) bool { return n > 1 && st.tab == n }

func (st *askState) answered(i int) bool { return len(st.picks[i]) > 0 || st.custom[i] != "" }

// firstUnanswered is the index of the first question with no answer, or -1 when
// every question is answered.
func (st *askState) firstUnanswered(n int) int {
	for i := range n {
		if !st.answered(i) {
			return i
		}
	}
	return -1
}

// answerAsk takes a key while a question panel is open.
func (m *model) answerAsk(it *Item, k string) (tea.Cmd, bool) {
	if it.Ask.Origin != nil && it.Ask.Origin.URL != "" {
		return m.answerURLAsk(it, k)
	}
	st := m.openAsk(it)
	qs := it.Ask.Questions
	if st.entering() {
		return m.typeAnswer(it, k)
	}
	if k == "esc" {
		// vi mode ignores Esc on the ask card: it does not dismiss. Only ^C
		// cancels the ask.
		if m.viActive() {
			return nil, true
		}
		return m.declineAsk(it), true
	}
	tabs := len(qs)
	if tabs > 1 {
		tabs++
	}
	switch k {
	case "left", "h":
		st.tab, st.cursor = max(st.tab-1, 0), 0
		return nil, true
	case "right", "l":
		// With auto-submit on the Submit tab is not drawn, so right stops at
		// the last question instead of landing on a hidden tab that commits.
		last := tabs - 1
		if m.opts.AutoSubmit {
			last = len(qs) - 1
		}
		st.tab, st.cursor = min(st.tab+1, last), 0
		return nil, true
	}
	if st.onSubmitTab(len(qs)) {
		if k == "enter" {
			return m.sendAsk(it), true
		}
		return nil, true
	}
	q := qs[st.tab]
	rows := len(q.Options) + 2
	switch {
	case k == "tab" && st.cursor < len(q.Options) && !q.Multi:
		m.openNote(q.Options[st.cursor].Label)
	case k == "up" || k == "k":
		st.cursor = (st.cursor + rows - 1) % rows
	case k == "down" || k == "j" || k == "tab":
		st.cursor = (st.cursor + 1) % rows
	case k == "space" && st.cursor < len(q.Options) && q.Multi:
		st.toggle(q.Options[st.cursor].Label)
	case k == "enter":
		return m.chooseRow(it, st.cursor), true
	case len(k) == 1 && k[0] >= '1' && k[0] <= '9':
		if n := int(k[0] - '1'); n < rows {
			st.cursor = n
			return m.chooseRow(it, n), true
		}
	default:
		return nil, false
	}
	return nil, true
}

func (st *askState) toggle(label string) {
	if i := slices.Index(st.picks[st.tab], label); i >= 0 {
		st.picks[st.tab] = slices.Delete(st.picks[st.tab], i, i+1)
		return
	}
	st.picks[st.tab] = append(st.picks[st.tab], label)
}

// chooseRow acts on a row: an option answers a single-choice question and
// toggles a multi-choice one, the typed row opens the composer, and the last
// row declines the whole panel.
func (m *model) chooseRow(it *Item, row int) tea.Cmd {
	st := m.ask
	q := it.Ask.Questions[st.tab]
	switch {
	case row < len(q.Options) && q.Multi:
		st.toggle(q.Options[row].Label)
		return nil
	case row < len(q.Options):
		if label := q.Options[row].Label; !slices.Equal(st.picks[st.tab], []string{label}) {
			st.picks[st.tab], st.notes[st.tab] = []string{label}, ""
		}
		st.custom[st.tab] = ""
		return m.nextQuestion(it)
	case row == len(q.Options):
		st.entry = entryAnswer
		m.composer.Placeholder = i18n.M.AskTypingHint
		m.composer.SetValue(st.custom[st.tab])
		return nil
	}
	return m.declineAsk(it)
}

// openNote picks a single-choice option and opens the composer for a note to
// send with it, for an option that asks for more than itself.
func (m *model) openNote(label string) {
	st := m.ask
	if !slices.Equal(st.picks[st.tab], []string{label}) {
		st.picks[st.tab], st.notes[st.tab] = []string{label}, ""
	}
	st.custom[st.tab] = ""
	st.entry = entryNote
	m.composer.Placeholder = i18n.M.AskNoteHint
	m.composer.SetValue(st.notes[st.tab])
}

// typeAnswer runs the composer while an entry is open: enter keeps what was
// typed as this question's answer or its pick's note, esc goes back to the rows.
func (m *model) typeAnswer(it *Item, k string) (tea.Cmd, bool) {
	st := m.ask
	switch k {
	case "esc":
		// vi mode ignores Esc while typing an ask answer: it does not back out
		// and discard the draft. Only ^C cancels the ask.
		if m.viActive() {
			return nil, true
		}
		st.entry = entryNone
	case "enter":
		text := strings.TrimSpace(m.composer.Value())
		if st.entry == entryNote {
			st.notes[st.tab] = text
		} else {
			st.custom[st.tab] = text
			if text != "" && !it.Ask.Questions[st.tab].Multi {
				st.picks[st.tab], st.notes[st.tab] = nil, ""
			}
		}
		st.entry = entryNone
		m.composer.Reset()
		m.composer.Placeholder = ""
		if st.answered(st.tab) {
			return m.nextQuestion(it), true
		}
		return nil, true
	default:
		return nil, false
	}
	m.composer.Reset()
	m.composer.Placeholder = ""
	return nil, true
}

func (m *model) nextQuestion(it *Item) tea.Cmd {
	st := m.ask
	n := len(it.Ask.Questions)
	if n == 1 {
		return m.sendAsk(it)
	}
	// With auto-submit on, answering moves on to the next unanswered question;
	// only answering the last one, with nothing left unanswered, commits — so a
	// question answered after a skip-back skips ahead to the next gap.
	if m.opts.AutoSubmit {
		if i := st.firstUnanswered(n); i >= 0 {
			st.tab, st.cursor = i, 0
			return nil
		}
		if st.tab == n-1 {
			return m.sendAsk(it)
		}
	}
	st.tab, st.cursor = st.tab+1, 0
	return nil
}

func (m *model) declineAsk(it *Item) tea.Cmd {
	for i := range m.ask.picks {
		m.ask.picks[i], m.ask.custom[i], m.ask.notes[i] = nil, "", ""
	}
	return m.sendAsk(it)
}

// sendAsk answers the panel. The verdict it seals the row with is what was
// answered, so the line left in the scrollback says it.
func (m *model) sendAsk(it *Item) tea.Cmd {
	answers := make([]AskAnswer, len(it.Ask.Questions))
	said := make([]string, 0, len(it.Ask.Questions))
	for i, q := range it.Ask.Questions {
		sel := m.ask.selected(i)
		answers[i] = AskAnswer{QuestionID: q.ID, Selected: sel}
		if len(sel) > 0 {
			said = append(said, strings.Join(sel, ", "))
		}
	}
	verdict := i18n.M.AskDeclined
	if len(said) > 0 {
		verdict = strings.Join(said, " · ")
	}
	m.ask = nil
	m.composer.Placeholder = ""
	m.tr.Decide(it.ID, verdict)
	id := it.Ask.ID
	return tea.Batch(m.commit(), m.call("answer", func(ctx context.Context) error { return m.client.Answer(ctx, id, answers) }))
}

// askPanel draws the question in view with its rows, or the review of every
// answer on the submit tab.
func (m *model) askPanel(it *Item) []string {
	if it.Ask.Origin != nil && it.Ask.Origin.URL != "" {
		return m.urlAskPanel(it)
	}
	st := m.openAsk(it)
	qs := it.Ask.Questions
	if len(qs) == 0 {
		return nil
	}
	var lines []string
	if len(qs) > 1 {
		lines = append(lines, m.askTabs(it), "")
	}
	if st.onSubmitTab(len(qs)) {
		lines = append(lines, termrender.Accent(i18n.M.AskSubmitTitle))
		for i, q := range qs {
			ans := termrender.Dim(i18n.M.AskUnanswered)
			if st.answered(i) {
				ans = strings.Join(st.selected(i), ", ")
			}
			lines = append(lines, "  "+termrender.Dim(header(q.Header, i))+": "+ans)
		}
		return panel(append(lines, termrender.Dim(i18n.M.AskSubmitHint)), m.width, accentEdge)
	}
	q := qs[st.tab]
	lines = append(lines, termrender.Accent("? ")+oneLine(q.Prompt, m.width-6))
	for j, o := range q.Options {
		box := ""
		if q.Multi {
			box = "☐ "
			if slices.Contains(st.picks[st.tab], o.Label) {
				box = "☑ "
			}
		}
		lines = append(lines, rowLine(st.cursor == j, j+1, box, oneLine(o.Label, m.width-12), false))
		if o.Description != "" {
			lines = append(lines, termrender.Dim("       "+oneLine(o.Description, m.width-10)))
		}
		if !q.Multi && slices.Equal(st.picks[st.tab], []string{o.Label}) {
			switch {
			case st.notes[st.tab] != "":
				lines = append(lines, termrender.Dim("       ↳ ")+oneLine(st.notes[st.tab], m.width-12))
			case st.entry == entryNote:
				lines = append(lines, termrender.Yellow("       ↳ "+i18n.M.AskNoteHint))
			}
		}
	}
	typed := len(q.Options)
	label := i18n.M.AskTypeSomething
	switch {
	case st.custom[st.tab] != "":
		label = st.custom[st.tab]
	case st.entry == entryAnswer:
		label = i18n.M.AskTypingHint
	}
	lines = append(lines,
		rowLine(st.cursor == typed, typed+1, "", oneLine(label, m.width-10), st.entry == entryAnswer && st.custom[st.tab] == ""),
		termrender.Dim(strings.Repeat("─", min(m.width-2, 40))),
		rowLine(st.cursor == typed+1, typed+2, "", i18n.M.AskChatInstead, false),
	)
	return panel(lines, m.width, accentEdge)
}

// selected is what question i answers: its picks, then a typed answer or the
// pick's note.
func (st *askState) selected(i int) []string {
	sel := slices.Clone(st.picks[i])
	for _, extra := range []string{st.custom[i], st.notes[i]} {
		if extra != "" {
			sel = append(sel, extra)
		}
	}
	return sel
}

func (m *model) askTabs(it *Item) string {
	st := m.ask
	qs := it.Ask.Questions
	parts := make([]string, 0, len(qs)+1)
	all := true
	for i, q := range qs {
		mark := "☐"
		if st.answered(i) {
			mark = "✔"
		} else {
			all = false
		}
		parts = append(parts, tabLabel(mark+" "+header(q.Header, i), i == st.tab))
	}
	if !m.opts.AutoSubmit {
		mark := "☐"
		if all {
			mark = "✔"
		}
		parts = append(parts, tabLabel(mark+" Submit", st.onSubmitTab(len(qs))))
	}
	return termrender.Dim("← ") + strings.Join(parts, "  ") + termrender.Dim(" →")
}

func tabLabel(s string, cur bool) string {
	if cur {
		return termrender.Reverse(" " + s + " ")
	}
	return termrender.Dim(s)
}

func header(h string, i int) string {
	if h != "" {
		return h
	}
	return fmt.Sprintf("Q%d", i+1)
}

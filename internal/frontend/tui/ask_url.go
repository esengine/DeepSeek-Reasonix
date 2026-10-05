package tui

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"reasonix/internal/base/i18n"
	"reasonix/internal/frontend/termrender"
)

type urlSubmission int

const (
	urlIdle urlSubmission = iota
	urlSending
	urlFailed
)

type urlAnswerMsg struct {
	item   int
	action string
	err    error
}

func (m *model) answerURLAsk(it *Item, k string) (tea.Cmd, bool) {
	if it.Verdict != "" {
		return nil, true
	}
	st := m.openAsk(it)
	if st.urlSubmission == urlSending {
		return nil, true
	}
	switch k {
	case "up":
		st.cursor = (st.cursor + 3) % 4
	case "down", "tab":
		st.cursor = (st.cursor + 1) % 4
	case "enter":
		return m.chooseURLAsk(it, st.cursor), true
	case "esc":
		if m.viActive() {
			return nil, true
		}
		return m.chooseURLAsk(it, 3), true
	}
	return nil, true
}

func (m *model) chooseURLAsk(it *Item, row int) tea.Cmd {
	if row == 0 {
		target := it.Ask.Origin.URL
		return m.call("open browser", func(ctx context.Context) error {
			if err := externalURLCommand(ctx, target).Run(); err != nil {
				return errors.New(i18n.M.AskURLOpenFailed)
			}
			return nil
		})
	}
	action := []string{"accept", "decline", "cancel"}[row-1]
	m.ask.urlSubmission = urlSending
	item, id := it.ID, it.Ask.ID
	answers := []AskAnswer{{QuestionID: it.Ask.Questions[0].ID, Selected: []string{action}}}
	return func() tea.Msg {
		return urlAnswerMsg{item: item, action: action, err: m.client.Answer(m.ctx, id, answers)}
	}
}

func (m *model) onURLAnswer(msg urlAnswerMsg) tea.Cmd {
	for _, it := range m.tr.Items {
		if it.ID == msg.item && it.Verdict != "" {
			return nil
		}
	}
	if m.ask != nil && m.ask.item == msg.item {
		if msg.err != nil {
			m.ask.urlSubmission = urlFailed
			return nil
		}
		m.ask = nil
	}
	if msg.err == nil {
		m.tr.Decide(msg.item, msg.action)
		return m.commit()
	}
	return nil
}

func externalURLCommand(ctx context.Context, target string) *exec.Cmd {
	switch runtime.GOOS {
	case "darwin":
		return exec.CommandContext(ctx, "open", target)
	case "windows":
		return exec.CommandContext(ctx, "rundll32", "url.dll,FileProtocolHandler", target)
	default:
		return exec.CommandContext(ctx, "xdg-open", target)
	}
}

func (m *model) urlAskPanel(it *Item) []string {
	st := m.openAsk(it)
	origin := it.Ask.Origin
	u, _ := url.Parse(origin.URL)
	lines := []string{termrender.Accent(fmt.Sprintf(i18n.M.AskURLSourceFmt, terminalURLText(origin.Source)))}
	lines = append(lines, wrapLines(i18n.M.AskURLHint, max(m.width-6, 10))...)
	lines = append(lines, wrapLines(terminalURLText(origin.Message), max(m.width-6, 10))...)
	lines = append(lines, termrender.Accent(origin.URLHost))
	lines = append(lines, wrapLines(origin.URL, max(m.width-6, 10))...)
	if u.Scheme != "https" || strings.Contains(origin.URLHost, "xn--") {
		lines = append(lines, termrender.Yellow(i18n.M.AskURLWarning))
	}
	if origin.URLLocal {
		lines = append(lines, termrender.Yellow(i18n.M.AskURLLocalWarning))
	}
	if st.urlSubmission == urlSending {
		return panel(append(lines, termrender.Dim(i18n.M.AskURLSending)), m.width, accentEdge)
	}
	if st.urlSubmission == urlFailed {
		lines = append(lines, termrender.Yellow(i18n.M.AskURLAnswerFailed))
	}
	for i, label := range []string{i18n.M.AskURLOpen, i18n.M.AskURLContinue, i18n.M.AskURLDecline, i18n.M.AskURLCancel} {
		row := termrender.Dim("  " + label)
		if st.cursor == i {
			row = termrender.Accent("❯ ") + termrender.Bold(label)
		}
		lines = append(lines, row)
	}
	return panel(lines, m.width, accentEdge)
}

func terminalURLText(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return ' '
		}
		return r
	}, ansi.Strip(s))
}

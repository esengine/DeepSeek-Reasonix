package cli

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"reasonix/internal/checkpoint"
	"reasonix/internal/control"
	"reasonix/internal/i18n"
)

// rewindPicker is the in-chat overlay for Esc-Esc / "/rewind". Stage 0 lists the
// session's turns (one checkpoint each); stage 1 picks what to restore for the
// chosen turn. It mirrors the chooser overlay: keys route through handleRewindKey
// and it renders via renderRewind while m.rewind is set.
type rewindPicker struct {
	metas []checkpoint.Meta
	sel   int // selected turn (index into metas)
	stage int // 0 = pick turn, 1 = pick scope
	scope int // index into rewindScopes (stage 1)
}

var rewindActions = []struct {
	kind  string // "scope" | "fork" | "summ-from" | "summ-upto"
	scope control.RewindScope
}{
	{"scope", control.RewindBoth},
	{"scope", control.RewindConversation},
	{"scope", control.RewindCode},
	{"fork", 0},
	{"summ-from", 0},
	{"summ-upto", 0},
}

// openRewind populates the picker from the session's checkpoints, selecting the
// most recent turn. A no-op (with a notice) when there is nothing to rewind.
func (m *chatTUI) openRewind() {
	metas := m.ctrl.Checkpoints()
	if len(metas) == 0 {
		m.notice(i18n.M.RewindNone)
		return
	}
	m.rewind = &rewindPicker{metas: metas, sel: len(metas) - 1}
}

func (m chatTUI) handleRewindKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	r := m.rewind
	switch msg.String() {
	case "esc":
		if r.stage == 1 {
			r.stage = 0
		} else {
			m.rewind = nil
		}
	case "up", "k":
		if r.stage == 0 {
			if r.sel > 0 {
				r.sel--
			}
		} else if r.scope > 0 {
			r.scope--
		}
	case "down", "j":
		if r.stage == 0 {
			if r.sel < len(r.metas)-1 {
				r.sel++
			}
		} else if r.scope < len(rewindActions)-1 {
			r.scope++
		}
	case "enter":
		if r.stage == 0 {
			r.stage = 1
		} else {
			return m.applyRewind()
		}
	case "b":
		if r.stage == 1 {
			r.scope = 0
			return m.applyRewind()
		}
	case "c":
		if r.stage == 1 {
			r.scope = 1
			return m.applyRewind()
		}
	case "d":
		if r.stage == 1 {
			r.scope = 2
			return m.applyRewind()
		}
	case "f":
		if r.stage == 1 {
			r.scope = 3
			return m.applyRewind()
		}
	case "s":
		if r.stage == 1 {
			r.scope = 4
			return m.applyRewind()
		}
	case "u":
		if r.stage == 1 {
			r.scope = 5
			return m.applyRewind()
		}
	}
	return m, nil
}

func (m chatTUI) applyRewind() (tea.Model, tea.Cmd) {
	r := m.rewind
	meta := r.metas[r.sel]
	act := rewindActions[r.scope]
	m.rewind = nil
	// The controller emits a notice for the outcome (success or failure) of each of
	// these, so the picker doesn't add its own — it would double on the CLI.
	switch act.kind {
	case "fork":
		if _, err := m.ctrl.Fork(meta.Turn); err == nil {
			m.replayActiveBranch(fmt.Sprintf("branched from turn %d", meta.Turn+1))
		}
		return m, nil // the branch is a new session
	case "summ-from":
		_ = m.ctrl.SummarizeFrom(context.Background(), meta.Turn)
		return m, nil
	case "summ-upto":
		_ = m.ctrl.SummarizeUpTo(context.Background(), meta.Turn)
		return m, nil
	}
	if err := m.ctrl.Rewind(meta.Turn, act.scope); err != nil {
		return m, nil
	}
	// The controller emits a notice marking the rewind point; the committed
	// transcript stays in terminal scrollback (v2 has no managed viewport), so for a
	// conversation/both rewind we prefill the composer with that turn's prompt to
	// re-send or edit — Claude Code's behavior — while the model's context is
	// truncated underneath.
	if act.scope != control.RewindCode && strings.TrimSpace(meta.Prompt) != "" {
		m.input.SetValue(meta.Prompt)
		m.growInputToFit()
	}
	return m, nil
}

func (m chatTUI) renderRewind() string {
	r := m.rewind
	if r == nil {
		return ""
	}
	w := max(m.width, 10)
	var b strings.Builder
	if r.stage == 0 {
		b.WriteString(accent(i18n.M.RewindPickTitle) + "\n")
		for i, meta := range r.metas {
			b.WriteString(rowLine(i == r.sel, meta.Turn+1, "", turnLabel(meta, w), false) + "\n")
		}
		b.WriteString(dim(i18n.M.RewindPickHint))
		return choicePanelStyle.Width(w).Render(b.String())
	}
	meta := r.metas[r.sel]
	b.WriteString(accent(fmt.Sprintf(i18n.M.RewindRestoreTitleFmt, meta.Turn+1)) + dim(oneLine(meta.Prompt, 48)) + "\n")
	for i := range rewindActions {
		b.WriteString(rowLine(i == r.scope, i+1, "", rewindActionLabel(i), false) + "\n")
	}
	b.WriteString(dim(i18n.M.RewindApplyHint))
	return choicePanelStyle.Width(w).Render(b.String())
}

func rewindActionLabel(i int) string {
	switch i {
	case 0:
		return i18n.M.RewindCodeConversation
	case 1:
		return i18n.M.RewindConversationOnly
	case 2:
		return i18n.M.RewindCodeOnly
	case 3:
		return i18n.M.RewindFork
	case 4:
		return i18n.M.RewindSummarizeFrom
	case 5:
		return i18n.M.RewindSummarizeUpto
	default:
		return ""
	}
}

func turnLabel(meta checkpoint.Meta, w int) string {
	label := oneLine(meta.Prompt, max(20, w-30))
	if n := len(meta.Paths); n > 0 {
		s := ""
		if n != 1 {
			s = "s"
		}
		label += dim(fmt.Sprintf("  (%d file%s)", n, s))
	}
	return label
}

// oneLine flattens s to a single line and truncates it to display width n.
func oneLine(s string, n int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if s == "" {
		return i18n.M.RewindEmpty
	}
	return ansi.Truncate(s, n, "…")
}

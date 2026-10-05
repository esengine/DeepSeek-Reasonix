package tui

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	tea "charm.land/bubbletea/v2"

	"reasonix/internal/base/i18n"
	"reasonix/internal/frontend/termrender"
)

// Checkpoint is one turn the session can be rewound to.
type Checkpoint struct {
	Turn   int    `json:"turn"`
	Prompt string `json:"prompt"`
	Files  int    `json:"files"`
}

// RewindPlan is the kernel's precheck of one rewind: what it can restore, and
// whether the files it would restore are only partly covered.
type RewindPlan struct {
	PlanID               string   `json:"planId"`
	CanFiles             bool     `json:"canFiles"`
	CanConversation      bool     `json:"canConversation"`
	DisabledReason       string   `json:"disabledReason"`
	CoverageGaps         []any    `json:"coverageGaps"`
	RequiresConfirmation bool     `json:"requiresConfirmation"`
	Files                []string `json:"files"`
}

// RewindResult is what a committed rewind changed.
type RewindResult struct {
	OK             bool `json:"ok"`
	ConversationOK bool `json:"conversationOk"`
}

func (c *Client) Checkpoints(ctx context.Context) ([]Checkpoint, error) {
	var out []Checkpoint
	err := c.do(ctx, http.MethodGet, "/checkpoints", nil, &out)
	return out, err
}

func (c *Client) PrepareRewind(ctx context.Context, turn int, scope string) (RewindPlan, error) {
	var out RewindPlan
	err := c.do(ctx, http.MethodPost, "/rewind/prepare", map[string]any{"turn": turn, "scope": scope}, &out)
	return out, err
}

func (c *Client) CommitRewind(ctx context.Context, planID string) (RewindResult, error) {
	var out RewindResult
	err := c.do(ctx, http.MethodPost, "/rewind/commit", map[string]string{"planId": planID}, &out)
	return out, err
}

// Summarize folds the conversation after ("from") or before ("upto") a turn.
func (c *Client) Summarize(ctx context.Context, turn int, mode string) error {
	return c.do(ctx, http.MethodPost, "/summarize", map[string]any{"turn": turn, "mode": mode}, nil)
}

// rewindAction is one row of the picker's second stage: a scope to restore,
// or a fold that keeps the history.
type rewindAction struct {
	key, scope, fold string
	label            func() string
}

var rewindActions = []rewindAction{
	{key: "b", scope: "both", label: func() string { return i18n.M.RewindCodeConversation }},
	{key: "c", scope: "conversation", label: func() string { return i18n.M.RewindConversationOnly }},
	{key: "d", scope: "code", label: func() string { return i18n.M.RewindCodeOnly }},
	{key: "s", fold: "from", label: func() string { return i18n.M.RewindSummarizeFrom }},
	{key: "u", fold: "upto", label: func() string { return i18n.M.RewindSummarizeUpto }},
}

// rewindPicker is the Esc-Esc and /rewind overlay. It lists the turns, then
// what to restore of the chosen one, then asks again when the files it would
// restore are only partly covered.
type rewindPicker struct {
	turns  []Checkpoint
	sel    int
	stage  int
	action int
	plan   RewindPlan
}

type (
	checkpointsMsg struct {
		turns []Checkpoint
		err   error
	}
	rewindPlanMsg struct {
		plan RewindPlan
		err  error
	}
	rewoundMsg struct {
		turn   Checkpoint
		scope  string
		result RewindResult
		err    error
	}
)

func (m *model) openRewind() tea.Cmd {
	return func() tea.Msg {
		turns, err := m.client.Checkpoints(m.ctx)
		return checkpointsMsg{turns: turns, err: err}
	}
}

// onCheckpoints opens the picker on the latest turn, the one most often meant.
func (m *model) onCheckpoints(msg checkpointsMsg) tea.Cmd {
	switch {
	case msg.err != nil:
		m.tr.AddNotice("error", "rewind: "+msg.err.Error())
		return m.commit()
	case len(msg.turns) == 0:
		m.tr.AddNotice("info", i18n.M.RewindNone)
		return m.commit()
	}
	m.rewind = &rewindPicker{turns: msg.turns, sel: len(msg.turns) - 1}
	return nil
}

// rewindKey takes every key while the picker is open.
func (m *model) rewindKey(k string) (tea.Cmd, bool) {
	r := m.rewind
	if r == nil {
		return nil, false
	}
	switch k {
	case "esc":
		if r.stage == 0 {
			m.rewind = nil
		} else {
			r.stage--
		}
	case "up", "k":
		r.move(-1)
	case "down", "j":
		r.move(1)
	case "enter", "y":
		switch {
		case r.stage == 0 && k == "enter":
			r.stage = 1
		case r.stage == 1 && k == "enter":
			return m.applyRewind(), true
		case r.stage == 2:
			return m.commitRewind(), true
		}
	default:
		for i, a := range rewindActions {
			if r.stage == 1 && k == a.key {
				r.action = i
				return m.applyRewind(), true
			}
		}
	}
	return nil, true
}

func (r *rewindPicker) move(d int) {
	switch r.stage {
	case 0:
		r.sel = min(max(r.sel+d, 0), len(r.turns)-1)
	case 1:
		r.action = min(max(r.action+d, 0), len(rewindActions)-1)
	}
}

// applyRewind runs the chosen action. A fold needs no precheck; a restore asks
// the kernel what it can do first.
func (m *model) applyRewind() tea.Cmd {
	r := m.rewind
	turn, act := r.turns[r.sel], rewindActions[r.action]
	if act.fold != "" {
		m.rewind = nil
		return m.call("summarize", func(ctx context.Context) error { return m.client.Summarize(ctx, turn.Turn, act.fold) })
	}
	return func() tea.Msg {
		plan, err := m.client.PrepareRewind(m.ctx, turn.Turn, act.scope)
		return rewindPlanMsg{plan: plan, err: err}
	}
}

func (m *model) onRewindPlan(msg rewindPlanMsg) tea.Cmd {
	r := m.rewind
	if r == nil {
		return nil
	}
	if msg.err != nil {
		m.rewind = nil
		m.tr.AddNotice("error", "rewind: "+msg.err.Error())
		return m.commit()
	}
	if !planCanApply(msg.plan, rewindActions[r.action].scope) {
		m.rewind = nil
		reason := strings.TrimSpace(msg.plan.DisabledReason)
		if reason == "" {
			reason = "precheck failed"
		}
		m.tr.AddNotice("warn", fmt.Sprintf(i18n.M.RewindUnavailableFmt, reason))
		return m.commit()
	}
	r.plan = msg.plan
	if msg.plan.RequiresConfirmation {
		r.stage = 2
		return nil
	}
	return m.commitRewind()
}

func planCanApply(p RewindPlan, scope string) bool {
	switch scope {
	case "code":
		return p.CanFiles
	case "conversation":
		return p.CanConversation
	}
	return p.CanFiles && p.CanConversation
}

func (m *model) commitRewind() tea.Cmd {
	r := m.rewind
	m.rewind = nil
	turn, scope, planID := r.turns[r.sel], rewindActions[r.action].scope, r.plan.PlanID
	return func() tea.Msg {
		res, err := m.client.CommitRewind(m.ctx, planID)
		return rewoundMsg{turn: turn, scope: scope, result: res, err: err}
	}
}

// onRewound redraws a conversation that went back, and hands the prompt of
// the turn it went back to the composer to edit, as 1.x does.
func (m *model) onRewound(msg rewoundMsg) tea.Cmd {
	if msg.err != nil || !msg.result.OK {
		if msg.err != nil {
			m.tr.AddNotice("error", "rewind: "+msg.err.Error())
		}
		return m.commit()
	}
	if msg.scope != "code" && strings.TrimSpace(msg.turn.Prompt) != "" {
		m.composer.SetValue(msg.turn.Prompt)
	}
	if !msg.result.ConversationOK {
		return nil
	}
	m.resetScreen()
	note := m.emit(func(int, bool) string {
		return "\n" + termrender.Dim(fmt.Sprintf("  -- rewound to turn %d --", msg.turn.Turn+1))
	})
	return tea.Sequence(note, m.greet(), m.fetchHistory(true), tea.Batch(m.fetchStatus(), m.fetchTodosForRebuild(), m.fetchMeters()))
}

func (m *model) rewindPanel() []string {
	r := m.rewind
	w := max(m.width-8, 20)
	var lines []string
	switch r.stage {
	case 0:
		lines = append(lines, termrender.Accent(i18n.M.RewindPickTitle))
		start := max(min(r.sel-pickerRows/2, len(r.turns)-pickerRows), 0)
		end := min(start+pickerRows, len(r.turns))
		if start > 0 {
			lines = append(lines, termrender.Dim("  ↑ more"))
		}
		for i := start; i < end; i++ {
			lines = append(lines, rowLine(i == r.sel, r.turns[i].Turn+1, "", turnLabel(r.turns[i], w), false))
		}
		if end < len(r.turns) {
			lines = append(lines, termrender.Dim("  ↓ more"))
		}
		lines = append(lines, termrender.Dim(i18n.M.RewindPickHint))
	case 1:
		t := r.turns[r.sel]
		lines = append(lines, termrender.Accent(fmt.Sprintf(i18n.M.RewindRestoreTitleFmt, t.Turn+1))+termrender.Dim(promptLine(t.Prompt, 48)))
		for i, a := range rewindActions {
			lines = append(lines, rowLine(i == r.action, i+1, "", a.label(), false))
		}
		lines = append(lines, termrender.Dim(i18n.M.RewindApplyHint))
	default:
		t := r.turns[r.sel]
		lines = append(lines, termrender.Accent(i18n.M.RewindCoverageTitle),
			fmt.Sprintf(i18n.M.RewindCoverageWarningFmt, len(r.plan.CoverageGaps)),
			termrender.Dim(fmt.Sprintf(i18n.M.RewindRestoreTitleFmt, t.Turn+1)+promptLine(t.Prompt, 48)),
			termrender.Dim(i18n.M.RewindConfirmHint))
	}
	return panel(lines, m.width, accentEdge)
}

func turnLabel(t Checkpoint, width int) string {
	label := promptLine(t.Prompt, max(20, width-30))
	if t.Files > 0 {
		s := "s"
		if t.Files == 1 {
			s = ""
		}
		label += termrender.Dim(fmt.Sprintf("  (%d file%s)", t.Files, s))
	}
	return label
}

func promptLine(s string, width int) string {
	if strings.TrimSpace(s) == "" {
		return i18n.M.RewindEmpty
	}
	return oneLine(s, width)
}

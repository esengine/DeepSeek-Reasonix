package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"reasonix/internal/base/i18n"
	"reasonix/internal/frontend/termrender"
)

const spinEvery = 100 * time.Millisecond

var spinFrames = []string{"⣾", "⣽", "⣻", "⢿", "⡿", "⣟", "⣯", "⣷"}

// Mode tag colors, fixed across themes so a mode reads the same everywhere.
var (
	tagAskColor   = termrender.NewColor("#f59e0b", 214)
	tagPlanColor  = termrender.NewColor("#2563eb", 27)
	tagYoloColor  = termrender.NewColor("#e5484d", 167)
	tagShellColor = termrender.NewColor("#16a34a", 71)
	tagViNormal   = termrender.NewColor("#475569", 240)
	tagViInsert   = termrender.NewColor("#0d9488", 30)
	tagLight      = termrender.NewColor("#ffffff", 231)
	tagDark       = termrender.NewColor("#111827", 234)
)

type spinMsg struct{}

func tickSpin() tea.Cmd {
	return tea.Tick(spinEvery, func(time.Time) tea.Msg { return spinMsg{} })
}

type bannerMsg struct{ s Status }

// greet puts the banner above anything else in the transcript; it asks for
// the status itself because the first frame has not been drawn.
func (m *model) greet() tea.Cmd {
	return func() tea.Msg {
		s, _ := m.client.Status(m.ctx)
		if m.scr == nil {
			return tea.Println(m.glyphs.apply(banner(s)))()
		}
		return bannerMsg{s: s}
	}
}

func banner(s Status) string {
	label := s.Label
	if label == "" {
		label = modelName(s.ModelRef)
	}
	head := termrender.Accent("◆") + " " + termrender.Bold("reasonix")
	if label != "" {
		head += "  " + termrender.Dim("· "+label)
	}
	return head + "\n" + termrender.Dim("  "+i18n.M.ChatTip)
}

// modelName drops the provider half of a ref that only repeats the model.
func modelName(ref string) string {
	if p, name, ok := strings.Cut(ref, "/"); ok && p == name {
		return name
	}
	return ref
}

// noteRunning starts the spinner on the frame a turn begins.
func (m *model) noteRunning(was bool) tea.Cmd {
	if !m.tr.Running {
		m.cancelling = false
	}
	if was || !m.tr.Running {
		return nil
	}
	m.runSince = time.Now()
	if m.spinning {
		return nil
	}
	m.spinning = true
	return tickSpin()
}

func (m *model) onSpin() tea.Cmd {
	if !m.tr.Running {
		m.spinning = false
		return nil
	}
	return tickSpin()
}

// workingLine says the turn is running, what phase it is in, and for how long.
func (m *model) workingLine() string {
	if !m.tr.Running || m.tr.OpenPrompt() != nil {
		return ""
	}
	since := time.Since(m.runSince)
	secs := int(since.Seconds())
	const mark = "\x00"
	var line string
	switch label := phaseLabel(m.tr.Phase); {
	case m.cancelling:
		stop := i18n.M.ChatStatusCancellingFmt
		if m.viActive() {
			stop = i18n.M.ChatStatusCancellingViFmt
		}
		line = fmt.Sprintf("  "+stop, mark, secs)
	case label != "":
		line = fmt.Sprintf("  %s %s · %ds", mark, label, secs)
	default:
		line = fmt.Sprintf("  "+i18n.M.ChatStatusThinkingFmt, mark, secs, m.interruptKey())
	}
	if m.tr.TurnOut > 0 {
		line += " · ↓" + shortTokens(m.tr.TurnOut)
	}
	before, after, _ := strings.Cut(line, mark)
	frame := spinFrames[int(since/spinEvery)%len(spinFrames)]
	return termrender.Dim(before) + termrender.Accent(frame) + termrender.Dim(after)
}

func phaseLabel(phase string) string {
	switch strings.ToLower(strings.TrimSpace(phase)) {
	case "working":
		return i18n.M.TurnPhaseWorking
	case "checking":
		return i18n.M.TurnPhaseChecking
	case "verifying":
		return i18n.M.TurnPhaseVerifying
	case "reviewing":
		return i18n.M.TurnPhaseReviewing
	}
	return ""
}

func shortTokens(n int) string {
	switch {
	case n >= 999_950:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fK", float64(n)/1_000)
	}
	return fmt.Sprint(n)
}

// panel frames lines between two full-width rules in edge, one cell in: the
// composer and the prompts that stand in for it share the frame.
func panel(lines []string, width int, edge func(string) string) []string {
	rule := edge(strings.Repeat("─", max(width, 1)))
	out := make([]string, 0, len(lines)+2)
	out = append(out, rule)
	for _, l := range lines {
		out = append(out, " "+l)
	}
	return append(out, rule)
}

func accentEdge(s string) string { return termrender.Accent(s) }

func shellEdge(s string) string { return termrender.ThemeFg(tagShellColor, s) }

// rowLine is a selectable row: "❯ N. label", bold where the cursor is, yellow
// where it is active, dim otherwise.
func rowLine(cur bool, num int, box, label string, active bool) string {
	return rowPrefix(cur) + rowStyle(cur, active)(fmt.Sprintf("%d. %s%s", num, box, label))
}

// markedRowLine is rowLine with a mark drawn in the accent outside the row's
// own style, so a dimmed row does not dim it.
func markedRowLine(cur bool, num int, mark, label string, active bool) string {
	style := rowStyle(cur, active)
	return rowPrefix(cur) + style(fmt.Sprintf("%d. ", num)) + termrender.Accent(mark) + style(" "+label)
}

func rowPrefix(cur bool) string {
	if cur {
		return termrender.Accent("❯ ")
	}
	return "  "
}

func rowStyle(cur, active bool) func(string) string {
	switch {
	case cur:
		return termrender.Bold
	case active:
		return termrender.Yellow
	default:
		return termrender.Dim
	}
}

func (m *model) composerLines() []string {
	edge, mark := accentEdge, termrender.Accent("❯ ")
	if m.shell {
		edge, mark = shellEdge, termrender.ThemeFg(tagShellColor, "! ")
	}
	rows := strings.Split(m.composer.View(), "\n")
	for i := range rows {
		if i == 0 {
			rows[i] = mark + rows[i]
		} else {
			rows[i] = "  " + rows[i]
		}
	}
	return panel(rows, m.width, edge)
}

func clipVisible(s string, width int) string {
	return termrender.Truncate(s, width, "…")
}

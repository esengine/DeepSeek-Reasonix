package cli

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"reasonix/internal/agent"
)

// turnTreePicker is the interactive overlay for /turntree. It displays the full
// conversation tree with each turn as a node, letting the user navigate with
// arrow keys and jump to any turn with Enter.
type turnTreePicker struct {
	nodes      []agent.TurnInfo
	sel        int
	cursor     int
	visible    int
	currentKey string
}

// openTurnTree populates the picker from the controller's TurnTree.
func (m *chatTUI) openTurnTree() {
	tree, err := m.ctrl.TurnTree()
	if err != nil {
		m.notice("turntree: " + err.Error())
		return
	}
	nodes := tree.FlattenCurrentRoot()
	if len(nodes) == 0 {
		m.notice("no conversation turns yet")
		return
	}
	sel := 0
	for i, n := range nodes {
		if n.IsCurrent {
			sel = i
			break
		}
	}
	m.turnTree = &turnTreePicker{
		nodes:      nodes,
		sel:        sel,
		cursor:     sel,
		visible:    12,
		currentKey: tree.CurrentKey,
	}
	m.scrollBottom = true
}

func (m chatTUI) handleTurnTreeKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	tt := m.turnTree
	if tt == nil {
		return m, nil
	}

	switch msg.String() {
	case "esc", "ctrl+c":
		m.turnTree = nil
		return m, nil
	case "up", "k":
		if tt.sel > 0 {
			tt.sel--
		}
	case "down", "j":
		if tt.sel < len(tt.nodes)-1 {
			tt.sel++
		}
	case "home":
		tt.sel = 0
	case "end":
		tt.sel = len(tt.nodes) - 1
	case "enter":
		return m.applyTurnTreeJump()
	}
	return m, nil
}

func (m chatTUI) applyTurnTreeJump() (tea.Model, tea.Cmd) {
	tt := m.turnTree
	if tt == nil || tt.sel >= len(tt.nodes) {
		return m, nil
	}
	node := tt.nodes[tt.sel]
	m.turnTree = nil

	if node.IsCurrent {
		m.notice("already at this turn")
		return m, nil
	}

	// JumpToTurn switches to a temporary preview; the next Send or /branch
	// materializes a real branch from this selected turn.
	if err := m.ctrl.JumpToTurn(node.BranchID, node.Turn); err != nil {
		m.notice("jump: " + err.Error())
		return m, nil
	}

	// Replay the truncated conversation in the transcript.
	m.finalizeStreamed()
	m.pending.Reset()
	m.reasoning.Reset()
	m.todoArgs = ""
	m.chooser = nil
	m.pendingApproval = nil
	m.bubblePending = false
	m.turnDiscarded = false

	m.commitLine("")
	m.commitLine(dim(fmt.Sprintf("  -- jumped to T%d: %s --", node.Turn+1, node.Prompt)))
	m.commitLine(strings.TrimRight(renderTUIBanner(m.label, "", m.width), "\n"))
	for _, section := range replaySectionsFor(m.ctrl.History(), m.width, m.renderer) {
		m.commitLine(strings.TrimRight(section, "\n"))
	}
	m.scrollBottom = true
	return m, nil
}

func (m chatTUI) renderTurnTree() string {
	tt := m.turnTree
	if tt == nil {
		return ""
	}

	w := max(m.width, 30)
	tt.visible = min(len(tt.nodes), 14)

	if tt.sel < tt.cursor {
		tt.cursor = tt.sel
	}
	if tt.sel >= tt.cursor+tt.visible {
		tt.cursor = tt.sel - tt.visible + 1
	}
	if tt.cursor < 0 {
		tt.cursor = 0
	}

	var b strings.Builder
	b.WriteString(accent("Conversation tree") + "  " + dim("↑↓ navigate  Enter jump  Esc close"))
	b.WriteString("\n")
	if current := turnTreeCurrentLabel(tt.nodes); current != "" {
		b.WriteString(dim("current: ") + current + "\n")
	}
	b.WriteString(dim(strings.Repeat("─", w)) + "\n")

	end := min(tt.cursor+tt.visible, len(tt.nodes))
	moreAbove := tt.cursor > 0
	moreBelow := end < len(tt.nodes)

	if moreAbove {
		b.WriteString(dim(fmt.Sprintf("  … %d more above\n", tt.cursor)))
	}

	for i := tt.cursor; i < end; i++ {
		n := tt.nodes[i]
		selected := i == tt.sel
		nextDepth := -1
		if i+1 < len(tt.nodes) {
			nextDepth = tt.nodes[i+1].Depth
		}
		b.WriteString(renderTurnTreeNode(n, selected, nextDepth, w) + "\n")
	}

	if moreBelow {
		b.WriteString(dim(fmt.Sprintf("  … %d more below", len(tt.nodes)-end)))
	}

	return choicePanelStyle.Width(w).Render(b.String())
}

func turnTreeCurrentLabel(nodes []agent.TurnInfo) string {
	for _, n := range nodes {
		if n.IsCurrent {
			return fmt.Sprintf("%s T%d %s", shortTurnBranch(n.BranchID), n.Turn+1, oneLineTurn(n.Prompt, 48))
		}
	}
	return ""
}

func renderTurnTreeNode(n agent.TurnInfo, selected bool, nextDepth int, width int) string {
	prefix := turnTreePrefix(n.Depth, nextDepth)

	currentMark := ""
	if n.IsCurrent {
		currentMark = accent(" @")
	}

	forkMark := ""
	if n.HasFork {
		if n.ChildCount > 1 {
			forkMark = dim(fmt.Sprintf(" [%d branches]", n.ChildCount))
		} else if n.ChildCount == 1 {
			forkMark = dim(" [1 branch]")
		}
	}
	prefixMark := dim(" · " + compactTurnChars(n.PrefixChars) + " chars")

	turnLabel := fmt.Sprintf("T%d:", n.Turn+1)
	prompt := oneLineTurn(n.Prompt, 0)
	maxPrompt := width - ansi.StringWidth(prefix) - ansi.StringWidth(turnLabel) - ansi.StringWidth(prefixMark) - 8
	if len(currentMark) > 0 {
		maxPrompt -= 3
	}
	if len(forkMark) > 0 {
		maxPrompt -= len(forkMark)
	}
	if maxPrompt < 10 {
		maxPrompt = 10
	}
	if r := []rune(prompt); len(r) > maxPrompt {
		prompt = string(r[:maxPrompt-1]) + "…"
	}
	if prompt == "" {
		prompt = "(empty)"
	}

	line := prefix + turnLabel + " " + prompt + prefixMark + forkMark + currentMark

	if selected {
		return accent("▸ ") + line
	}
	return "  " + line
}

func turnTreePrefix(depth, nextDepth int) string {
	if depth <= 0 {
		if nextDepth > 0 {
			return "●─"
		}
		return "● "
	}
	var b strings.Builder
	for i := 0; i < depth-1; i++ {
		b.WriteString("│  ")
	}
	if nextDepth >= depth {
		b.WriteString("├─")
	} else {
		b.WriteString("└─")
	}
	return dim(b.String())
}

func oneLineTurn(s string, maxRunes int) string {
	s = strings.Join(strings.Fields(s), " ")
	if maxRunes <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) <= maxRunes {
		return s
	}
	if maxRunes <= 1 {
		return string(r[:maxRunes])
	}
	return string(r[:maxRunes-1]) + "…"
}

func shortTurnBranch(id string) string {
	if id == "" {
		return "branch"
	}
	if len(id) > 18 {
		return id[:18]
	}
	return id
}

func compactTurnChars(n int) string {
	if n < 0 {
		n = 0
	}
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	if n < 10000 {
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return fmt.Sprintf("%dk", n/1000)
}

package cli

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"reasonix/internal/agent"
	"reasonix/internal/control"
)

func (m *chatTUI) showBranchTree() {
	branches, err := m.ctrl.Branches()
	if err != nil {
		m.notice("tree: " + err.Error())
		return
	}
	current := agent.BranchID(m.ctrl.SessionPath())
	tree := renderBranchTree(control.FormatBranchTree(branches, current))
	m.commitLine(ansi.Hardwrap(tree, max(m.width, 20), false))
}

func renderBranchTree(tree string) string {
	lines := strings.Split(tree, "\n")
	for i, line := range lines {
		lines[i] = renderBranchTreeLine(line)
	}
	return strings.Join(lines, "\n")
}

func renderBranchTreeLine(line string) string {
	if line == "branches:" {
		return accent(line)
	}
	joint := strings.LastIndex(line, "├─ ")
	if alt := strings.LastIndex(line, "└─ "); alt > joint {
		joint = alt
	}
	if joint < 0 {
		return line
	}
	treePrefix := line[:joint+len("├─ ")]
	parts := strings.SplitN(line[joint+len("├─ "):], "  ", 3)
	if len(parts) < 3 {
		return line
	}
	id, title, meta := parts[0], parts[1], parts[2]

	turns := meta
	current := ""
	if before, after, ok := strings.Cut(meta, "  "); ok {
		turns = before
		if strings.TrimSpace(after) == "current" {
			current = "  " + accent("current")
		} else if strings.TrimSpace(after) != "" {
			current = "  " + after
		}
	}
	return dim(treePrefix) + dim(id) + "  " + title + "  " + dim(turns) + current
}

func (m *chatTUI) runBranchCommand(input string) {
	cmd := strings.Fields(input)[0]
	args := strings.TrimSpace(strings.TrimPrefix(input, cmd))

	// /branch 3 optional-name branches from displayed turn 3. Plain /branch
	// branches from the current tip.
	if n, name, fromTurn, err := control.ParseBranchTarget(args); err != nil {
		m.notice(err.Error())
		return
	} else if fromTurn {
		if _, err := m.ctrl.ForkNamed(n-1, name); err != nil {
			return
		}
		m.replayActiveBranch(fmt.Sprintf("branched from turn %d", n))
		return
	} else {
		if _, err := m.ctrl.Branch(name); err != nil {
			return
		}
	}
	m.showBranchTree()
}

func (m *chatTUI) runSwitchCommand(input string) {
	ref := strings.TrimSpace(strings.TrimPrefix(input, strings.Fields(input)[0]))
	if ref == "" {
		m.notice("usage: /switch <branch id|name>")
		return
	}
	if _, err := m.ctrl.SwitchBranch(ref); err != nil {
		return
	}
	m.replayActiveBranch("switched branch")
}

func (m *chatTUI) replayActiveBranch(title string) {
	m.finalizeStreamed()
	m.pending.Reset()
	m.reasoning.Reset()
	m.todoArgs = ""
	m.chooser = nil
	m.pendingApproval = nil
	m.bubblePending = false
	m.turnDiscarded = false

	m.commitLine("")
	if title != "" {
		m.commitLine(dim("  -- " + title + " --"))
	}
	m.commitLine(strings.TrimRight(renderTUIBanner(m.label, "", m.width), "\n"))
	for _, section := range replaySectionsFor(m.ctrl.History(), m.width, m.renderer) {
		m.commitLine(strings.TrimRight(section, "\n"))
	}
}

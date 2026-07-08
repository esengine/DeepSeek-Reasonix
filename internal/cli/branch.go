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
	m.followSessionLease()
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
			m.followSessionLease()
			return
		}
		m.followSessionLease()
		m.replayActiveBranch(fmt.Sprintf("branched from turn %d", n))
		return
	} else {
		if _, err := m.ctrl.Branch(name); err != nil {
			m.followSessionLease()
			return
		}
		m.followSessionLease()
	}
	m.showBranchTree()
}

func (m *chatTUI) runSwitchCommand(input string) {
	ref := strings.TrimSpace(strings.TrimPrefix(input, strings.Fields(input)[0]))
	if ref == "" {
		m.notice("usage: /switch <branch id|name>")
		return
	}
	// Move the session lease before the controller binds the target branch for
	// writing; a branch held by another runtime is refused here. Resolution
	// failures fall through to SwitchBranch, which reports them as before.
	if m.leases != nil {
		if branches, err := m.ctrl.Branches(); err == nil {
			m.followSessionLease()
			if match, err := control.ResolveBranchRef(branches, ref); err == nil {
				if err := m.rebindSessionLease(match.Path); err != nil {
					m.notice("switch: " + sessionLeaseHeldNotice(err))
					return
				}
			}
		} else {
			m.followSessionLease()
		}
	}
	if _, err := m.ctrl.SwitchBranch(ref); err != nil {
		// The switch failed after the lease already moved; re-point it at the
		// session the controller still owns.
		m.restoreSessionLease()
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
	m.planMode = false
	m.ctrl.SetPlanMode(false)
	m.sessionSwitch = true

	// Discard the previous session's transcript so the viewport only shows the
	// newly loaded session. Without this the transcript accumulates across
	// every /resume / /switch / /rewind / /branch, bloating memory and causing
	// the scroll position to be preserved at a stale offset inside the merged
	// content (#4584).
	m.clearTranscriptDisplay()
	m.transcriptDirty = true
	m.forceGotoBottom = true

	m.commitLine("")
	if title != "" {
		m.commitLine(dim("  -- " + title + " --"))
	}
	contentW := transcriptContentWidth(m.width, m.nativeScrollback)
	m.commitLine(strings.TrimRight(renderTUIBanner(m.label, "", contentW), "\n"))
	for _, section := range replaySectionsFor(m.ctrl.History(), contentW, m.renderer) {
		m.commitLine(strings.TrimRight(section, "\n"))
	}
}

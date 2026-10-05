package tui

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"reasonix/internal/base/i18n"
	"reasonix/internal/contract/eventwire"
	"reasonix/internal/frontend/termrender"
)

const approvalSubjectRows = 8

// approvalChoice is one row of an approval panel. key is its shortcut; a plan
// row carries the plan action, a tool row the grant it answers with.
type approvalChoice struct {
	label, key, verdict string
	allow, session      bool
	persist             bool
}

// approvalChoices lists the answers the host said it will honour, in the
// catalog's order, labelled from the catalog.
func (m *model) approvalChoices(a *eventwire.Approval) []approvalChoice {
	if a.Kind == "plan" {
		l := choiceLabels(m.viHint(i18n.M.PlanApprovalChoices, i18n.M.PlanApprovalChoicesVi), 3)
		return []approvalChoice{
			{label: l[0], key: "y", verdict: "start_execution"},
			{label: l[1], key: "n", verdict: "revise_plan"},
			{label: l[2], key: "x", verdict: "exit_plan"},
		}
	}
	scope := a.Scope
	if scope == "" {
		scope = termrender.ToolDisplayName(a.Tool)
	}
	tmpl := i18n.M.ToolApprovalChoices
	l := choiceLabels(fmt.Sprintf(tmpl, repeatArg(scope, strings.Count(tmpl, "%s"))...), 4)
	out := []approvalChoice{{label: l[0], key: "y", verdict: "once", allow: true}}
	if a.AllowsSession {
		out = append(out, approvalChoice{label: l[1], key: "a", verdict: "session", allow: true, session: true})
	}
	if a.AllowsPersist {
		out = append(out, approvalChoice{label: l[2], key: "p", verdict: "always", allow: true, persist: true})
	}
	return append(out, approvalChoice{label: l[3], key: "n", verdict: "deny"})
}

func repeatArg(s string, n int) []any {
	out := make([]any, n)
	for i := range out {
		out[i] = s
	}
	return out
}

// choiceLabels reads the "N. label" lines of a catalog choice list.
func choiceLabels(list string, n int) []string {
	var out []string
	for line := range strings.SplitSeq(list, "\n") {
		line = strings.TrimSpace(line)
		if len(line) >= 3 && line[0] >= '1' && line[0] <= '9' && line[1] == '.' {
			out = append(out, strings.TrimSpace(line[2:]))
		}
	}
	for len(out) < n {
		out = append(out, "")
	}
	return out
}

// approvalSel is the row the cursor is on in the open approval panel.
type approvalSel struct {
	item, row int
}

func (m *model) approvalRow(it *Item) int {
	if m.apSel.item != it.ID {
		m.apSel = approvalSel{item: it.ID}
	}
	return m.apSel.row
}

// answerApproval takes the keys an approval panel owns: the cursor, enter,
// a row's number, and each row's shortcut. Esc answers as the last row does.
func (m *model) answerApproval(it *Item, k string) (tea.Cmd, bool) {
	choices := m.approvalChoices(it.Approval)
	row := m.approvalRow(it)
	switch {
	case k == "up":
		m.apSel.row = (row + len(choices) - 1) % len(choices)
		return nil, true
	case k == "down" || k == "tab":
		m.apSel.row = (row + 1) % len(choices)
		return nil, true
	case k == "enter":
		return m.decideApproval(it, choices[row]), true
	case k == "esc":
		// vi mode ignores Esc on the approval card: a user hitting it out of
		// habit must not reject the call. A row key or ^C decides instead.
		if m.viActive() {
			return nil, true
		}
		if it.Approval.Kind == "plan" {
			return m.decideApproval(it, choices[1]), true
		}
		return m.decideApproval(it, choices[len(choices)-1]), true
	case len(k) == 1 && k[0] >= '1' && k[0] <= '9':
		if n := int(k[0] - '1'); n < len(choices) {
			return m.decideApproval(it, choices[n]), true
		}
		return nil, true
	}
	for _, c := range choices {
		if c.key == k {
			return m.decideApproval(it, c), true
		}
	}
	return nil, false
}

func (m *model) decideApproval(it *Item, c approvalChoice) tea.Cmd {
	m.tr.Decide(it.ID, c.verdict)
	id := it.Approval.ID
	if it.Approval.Kind == "plan" {
		return tea.Batch(m.commit(), m.call("plan", func(ctx context.Context) error {
			err := m.client.PlanDecision(ctx, id, c.verdict)
			if Code(err) == CodePlanStale {
				return nil
			}
			return err
		}))
	}
	return tea.Batch(m.commit(), m.call("approve", func(ctx context.Context) error {
		return m.client.Approve(ctx, id, c.allow, c.session, c.persist)
	}))
}

// approvalPanel stands in for the composer while a call waits on the user.
func (m *model) approvalPanel(it *Item) []string {
	a := it.Approval
	width := max(m.width-2, 20)
	var text []string
	if a.Kind == "plan" {
		text = []string{approvalTitle(i18n.M.PlanApprovalPrompt)}
	} else {
		name, detail := approvalToolDetails(a.Tool)
		subject := strings.TrimSpace(a.Subject)
		preview, shown := "", ""
		if subject != "" {
			shown = oneLine(subject, max(width-28, 16))
			preview = " " + termrender.Accent(shown)
		}
		body := strings.TrimSpace(fmt.Sprintf(i18n.M.ToolApprovalPromptFmt, name, preview, detail, ""))
		if reason := strings.TrimSpace(a.Reason); reason != "" {
			body += " · " + oneLine(reason, width)
		}
		text = strings.Split(body, "\n")
		text[0] = approvalTitle(text[0])
		if shown != subject {
			text = append(text, subjectRows(subject, width)...)
		}
	}
	row := m.approvalRow(it)
	for i, c := range m.approvalChoices(a) {
		text = append(text, rowLine(i == row, i+1, "", c.label, false))
	}
	text = append(text, termrender.Dim(i18n.M.ApprovalChoiceHint))
	return panel(text, m.width, accentEdge)
}

// approvalTitle heads the panel in its accent, with no pictograph: a terminal
// that draws one as a two-cell emoji where the width tables say one cell
// overprints the text beside it.
func approvalTitle(s string) string { return termrender.Bold(termrender.Accent(s)) }

// subjectRows shows a subject the one-line preview had to clip, because the
// part it cut can be the part that matters.
func subjectRows(subject string, width int) []string {
	lines := strings.Split(subject, "\n")
	if len(lines) > approvalSubjectRows {
		lines = append(lines[:approvalSubjectRows-1], "…")
	}
	for i, l := range lines {
		lines[i] = clipVisible(l, width)
	}
	return lines
}

// approvalToolDetails names a tool the way the user knows it: an MCP tool by
// its short name, with the server it comes from as its source.
func approvalToolDetails(tool string) (name, detail string) {
	if rest, ok := strings.CutPrefix(tool, "mcp__"); ok {
		if server, short, ok := strings.Cut(rest, "__"); ok {
			return short, fmt.Sprintf(i18n.M.ToolApprovalSourceFmt, server)
		}
	}
	return termrender.ToolDisplayName(tool), fmt.Sprintf(i18n.M.ToolApprovalSourceFmt, i18n.M.ToolApprovalBuiltIn)
}

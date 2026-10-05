package plancontract

import (
	"fmt"
	"strings"
)

// Render turns a plan into the markdown a user reads. Its only list items are
// the steps — phases numbered, sub-steps indented — so a reader that parses the
// text for a task list finds exactly what ProjectTodos builds. RequiresApproval
// is absent on purpose: it is a routing request the host answers with its
// approval surface, not plan content.
func Render(p Plan) string {
	p = p.Normalize()
	var b strings.Builder
	if p.Objective != "" {
		fmt.Fprintf(&b, "**Objective** — %s\n", p.Objective)
	}
	renderAssumptions(&b, p.Assumptions)
	if len(p.NonGoals) > 0 {
		section(&b, "Non-goals")
		for _, goal := range p.NonGoals {
			fmt.Fprintf(&b, "  %s\n", continuation(goal))
		}
	}
	renderSteps(&b, p.Ordered())
	return strings.TrimSpace(b.String())
}

func renderAssumptions(b *strings.Builder, assumptions []Assumption) {
	if len(assumptions) == 0 {
		return
	}
	section(b, "Assumptions")
	for _, a := range assumptions {
		if a.Confirm == "" {
			fmt.Fprintf(b, "  %s\n", continuation(a.Text))
			continue
		}
		fmt.Fprintf(b, "  %s (confirm: %s)\n", continuation(a.Text), a.Confirm)
	}
}

// continuation strips a leading list marker from text that renders without a
// label, so a planner that writes its assumptions as bullets cannot smuggle a
// line into the step list.
func continuation(s string) string {
	for _, marker := range []string{"- ", "* ", "+ "} {
		if rest, ok := strings.CutPrefix(s, marker); ok {
			return strings.TrimSpace(rest)
		}
	}
	digits := 0
	for digits < len(s) && s[digits] >= '0' && s[digits] <= '9' {
		digits++
	}
	if digits > 0 && digits+1 < len(s) && (s[digits] == '.' || s[digits] == ')') && s[digits+1] == ' ' {
		return strings.TrimSpace(s[digits+2:])
	}
	return s
}

func renderSteps(b *strings.Builder, steps []Step) {
	if len(steps) == 0 {
		return
	}
	section(b, "Plan")
	phase := 0
	for _, step := range steps {
		if step.ParentID == "" {
			phase++
			fmt.Fprintf(b, "%d. %s\n", phase, step.Title)
			renderDetail(b, step, "   ")
			continue
		}
		fmt.Fprintf(b, "   - %s\n", step.Title)
		renderDetail(b, step, "     ")
	}
}

// renderDetail writes a step's evidence and checks as indented continuation
// lines. They carry no list marker on purpose: a reader parsing the plan for its
// task list must see steps and nothing else.
func renderDetail(b *strings.Builder, step Step, indent string) {
	if len(step.VerifiedFiles) > 0 {
		fmt.Fprintf(b, "%sverified: %s\n", indent, strings.Join(step.VerifiedFiles, ", "))
	}
	if len(step.CandidateFiles) > 0 {
		fmt.Fprintf(b, "%scandidate: %s\n", indent, strings.Join(step.CandidateFiles, ", "))
	}
	for _, c := range step.Acceptance {
		label := "accept"
		if c.Regression {
			label = "regression"
		}
		if c.Optional {
			label += " (optional)"
		}
		// The id is rendered because a proof has to cite it: a criterion the
		// executor cannot name is one it cannot satisfy.
		fmt.Fprintf(b, "%s%s [%s]: %s\n", indent, label, c.ID, c.Text)
	}
	for _, v := range step.Verification {
		command := v.Command
		if command == "" {
			command = "any verification command"
		}
		if v.Expect != "" {
			command += " — " + v.Expect
		}
		fmt.Fprintf(b, "%sverify: %s\n", indent, command)
	}
	for _, risk := range step.Risks {
		fmt.Fprintf(b, "%srisk: %s\n", indent, risk)
	}
}

func section(b *strings.Builder, title string) {
	if b.Len() > 0 {
		b.WriteString("\n")
	}
	fmt.Fprintf(b, "**%s**\n", title)
}

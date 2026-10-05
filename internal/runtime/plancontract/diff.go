package plancontract

import (
	"fmt"
	"slices"
	"strings"
)

// Diff is what one revision changed. Steps pair by id, which is why identity is
// host-assigned and never regenerated: a diff that pairs by position or title
// cannot tell a step that moved from a step that was replaced.
type Diff struct {
	FromRevision int
	ToRevision   int
	Objective    *TextChange
	Added        []Step
	Removed      []Step
	Changed      []StepChange
	Preserved    []Step
}

// TextChange is a plan-level field that moved.
type TextChange struct{ Before, After string }

// StepChange names which parts of a step moved, because "the title was reworded"
// and "the acceptance criteria were rewritten" carry very different risk.
type StepChange struct {
	Before Step
	After  Step
	Fields []string
}

// Compare pairs the two revisions by step id. It is a pure function so the host
// can render it, gate on it, and record it from the same result.
func Compare(before, after Plan) Diff {
	before, after = before.Normalize(), after.Normalize()
	d := Diff{FromRevision: before.Revision, ToRevision: after.Revision}
	if before.Objective != after.Objective {
		d.Objective = &TextChange{Before: before.Objective, After: after.Objective}
	}
	prev := make(map[string]Step, len(before.Steps))
	for _, step := range before.Steps {
		prev[step.ID] = step
	}
	for _, step := range after.Ordered() {
		old, existed := prev[step.ID]
		delete(prev, step.ID)
		if !existed {
			d.Added = append(d.Added, step)
			continue
		}
		if fields := changedFields(old, step); len(fields) > 0 {
			d.Changed = append(d.Changed, StepChange{Before: old, After: step, Fields: fields})
			continue
		}
		d.Preserved = append(d.Preserved, step)
	}
	for _, step := range before.Ordered() {
		if _, gone := prev[step.ID]; gone {
			d.Removed = append(d.Removed, step)
		}
	}
	return d
}

func changedFields(before, after Step) []string {
	var fields []string
	add := func(name string, same bool) {
		if !same {
			fields = append(fields, name)
		}
	}
	add("title", before.Title == after.Title)
	add("phase", before.ParentID == after.ParentID)
	add("depends_on", slices.Equal(before.DependsOn, after.DependsOn))
	add("verified_files", slices.Equal(before.VerifiedFiles, after.VerifiedFiles))
	add("candidate_files", slices.Equal(before.CandidateFiles, after.CandidateFiles))
	add("acceptance", slices.Equal(before.Acceptance, after.Acceptance))
	add("verification", slices.Equal(before.Verification, after.Verification))
	add("risks", slices.Equal(before.Risks, after.Risks))
	return fields
}

// NeedsApproval reports whether the revision expands what the user agreed to.
// Narrowing, reordering, retitling, and adding evidence never do: re-asking for
// those trains the user to approve without reading, which costs more than the
// gate saves.
func (d Diff) NeedsApproval() bool {
	if d.Objective != nil || len(d.Added) > 0 {
		return true
	}
	for _, change := range d.Changed {
		if grew(change.Before.Risks, change.After.Risks) ||
			grew(change.Before.Acceptance, change.After.Acceptance) ||
			grew(change.Before.VerifiedFiles, change.After.VerifiedFiles) ||
			grew(change.Before.CandidateFiles, change.After.CandidateFiles) {
			return true
		}
	}
	return false
}

func grew[T any](before, after []T) bool { return len(after) > len(before) }

// Moved reports whether anything actually changed. Preserved alone is not a
// change — it is the reassurance that sits beside one.
func (d Diff) Moved() bool {
	return d.Objective != nil || len(d.Added) > 0 || len(d.Removed) > 0 || len(d.Changed) > 0
}

// RenderDiff turns the comparison into the summary a reviewer reads. Empty
// sections are omitted, and a revision that changed nothing renders as nothing.
func RenderDiff(d Diff) string {
	if !d.Moved() {
		return ""
	}
	var b strings.Builder
	if d.FromRevision > 0 && d.ToRevision > 0 {
		fmt.Fprintf(&b, "**Revision %d → %d**\n", d.FromRevision, d.ToRevision)
	}
	if d.Objective != nil {
		fmt.Fprintf(&b, "\n**Objective**\n  was: %s\n  now: %s\n", d.Objective.Before, d.Objective.After)
	}
	diffSection(&b, "Added", d.Added, func(s Step) string { return s.ID + "  " + s.Title })
	diffSection(&b, "Removed", d.Removed, func(s Step) string { return s.ID + "  " + s.Title })
	diffSection(&b, "Changed", d.Changed, func(c StepChange) string {
		return c.After.ID + "  " + c.After.Title + " (" + strings.Join(c.Fields, ", ") + ")"
	})
	diffSection(&b, "Preserved", d.Preserved, func(s Step) string { return s.ID + "  " + s.Title })
	return strings.TrimSpace(b.String())
}

func diffSection[T any](b *strings.Builder, title string, items []T, line func(T) string) {
	if len(items) == 0 {
		return
	}
	fmt.Fprintf(b, "\n**%s**\n", title)
	for _, item := range items {
		fmt.Fprintf(b, "  %s\n", line(item))
	}
}

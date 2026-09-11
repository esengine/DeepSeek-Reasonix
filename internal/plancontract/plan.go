package plancontract

import (
	"errors"
	"fmt"
	"strings"
)

// MaxSteps bounds a plan's total step count. A plan past it is malformed rather
// than merely long: rejecting it beats projecting a silently truncated list.
const MaxSteps = 50

// Plan is one revision of a proposed approach. RequiresApproval is the planner's
// request, never its decision — the host's route owns whether execution gates.
// ID and Revision carry json:"-" because they are host-assigned: a planner
// cannot claim an identity the host did not give it.
type Plan struct {
	ID               string       `json:"-"`
	Revision         int          `json:"-"`
	Objective        string       `json:"objective"`
	Assumptions      []Assumption `json:"assumptions,omitempty"`
	NonGoals         []string     `json:"non_goals,omitempty"`
	Steps            []Step       `json:"steps"`
	RequiresApproval bool         `json:"requires_approval,omitempty"`
}

// Assumption is an unverified premise the plan rests on. Confirm names the
// cheapest check that would settle it; empty leaves that to the executor.
type Assumption struct {
	Text    string `json:"text"`
	Confirm string `json:"confirm,omitempty"`
}

// Step is one unit of the plan. An empty ParentID makes it a phase, otherwise it
// is a sub-step of that phase. DependsOn orders siblings and is advisory: the
// todo projection is serial, so a dependency becomes order, not gating.
type Step struct {
	ID             string         `json:"id,omitempty"`
	ParentID       string         `json:"parent_id,omitempty"`
	Title          string         `json:"title"`
	DependsOn      []string       `json:"depends_on,omitempty"`
	VerifiedFiles  []string       `json:"verified_files,omitempty"`  // paths the planner read
	CandidateFiles []string       `json:"candidate_files,omitempty"` // paths the planner inferred
	Acceptance     []Criterion    `json:"acceptance,omitempty"`
	Verification   []Verification `json:"verification,omitempty"`
	Risks          []string       `json:"risks,omitempty"`
}

// Criterion is one acceptance criterion. Regression marks must-keep-passing
// behavior; Optional marks a nice-to-have that never blocks completion. ID is
// host-assigned so it can key an evidence requirement downstream.
type Criterion struct {
	ID         string `json:"-"`
	Text       string `json:"text"`
	Regression bool   `json:"regression,omitempty"`
	Optional   bool   `json:"optional,omitempty"`
}

// Verification is a command-level check. An empty Command accepts any
// delivery-verification command; Expect describes what a pass looks like.
type Verification struct {
	Command string `json:"command,omitempty"`
	Expect  string `json:"expect,omitempty"`
}

// Normalize returns a canonical copy: trimmed text, dropped empty entries,
// assigned IDs, and repaired parent and dependency references. It never fails —
// what it cannot repair is left for Validate to reject.
func (p Plan) Normalize() Plan {
	out := Plan{
		ID:               strings.TrimSpace(p.ID),
		Revision:         max(p.Revision, 1),
		Objective:        strings.TrimSpace(p.Objective),
		NonGoals:         cleanStrings(p.NonGoals),
		RequiresApproval: p.RequiresApproval,
	}
	for _, a := range p.Assumptions {
		text := strings.TrimSpace(a.Text)
		if text == "" {
			continue
		}
		out.Assumptions = append(out.Assumptions, Assumption{Text: text, Confirm: strings.TrimSpace(a.Confirm)})
	}
	out.Steps = normalizeSteps(p.Steps)
	return out
}

func normalizeSteps(steps []Step) []Step {
	out := make([]Step, 0, len(steps))
	for _, s := range steps {
		title := strings.TrimSpace(s.Title)
		if title == "" {
			continue
		}
		out = append(out, Step{
			ID:             strings.TrimSpace(s.ID),
			ParentID:       strings.TrimSpace(s.ParentID),
			Title:          title,
			DependsOn:      cleanStrings(s.DependsOn),
			VerifiedFiles:  cleanStrings(s.VerifiedFiles),
			CandidateFiles: cleanStrings(s.CandidateFiles),
			Acceptance:     cleanCriteria(s.Acceptance),
			Verification:   cleanVerifications(s.Verification),
			Risks:          cleanStrings(s.Risks),
		})
	}
	assignStepIDs(out)
	assignCriterionIDs(out)
	repairParents(out)
	repairDependencies(out)
	return out
}

// assignStepIDs fills blank and duplicate IDs with "s<n>", leaving the first
// use of an ID with its submitted value so parent and dependency references the
// planner did write keep resolving.
func assignStepIDs(steps []Step) {
	used := make(map[string]bool, len(steps))
	for i := range steps {
		id := steps[i].ID
		if id == "" || used[id] {
			steps[i].ID = ""
			continue
		}
		used[id] = true
	}
	next := 1
	for i := range steps {
		if steps[i].ID != "" {
			continue
		}
		for {
			id := fmt.Sprintf("plan_step_%02d", next)
			next++
			if !used[id] {
				used[id] = true
				steps[i].ID = id
				break
			}
		}
	}
}

func assignCriterionIDs(steps []Step) {
	used := make(map[string]bool)
	for i := range steps {
		for j := range steps[i].Acceptance {
			id := steps[i].Acceptance[j].ID
			if id == "" || used[id] {
				steps[i].Acceptance[j].ID = ""
				continue
			}
			used[id] = true
		}
	}
	next := 1
	for i := range steps {
		for j := range steps[i].Acceptance {
			if steps[i].Acceptance[j].ID != "" {
				continue
			}
			for {
				id := fmt.Sprintf("c%d", next)
				next++
				if !used[id] {
					used[id] = true
					steps[i].Acceptance[j].ID = id
					break
				}
			}
		}
	}
}

// repairParents rewrites every parent reference to the top-level phase the step
// actually belongs to, so a normalized plan is literally two levels deep and
// Ordered reads the field instead of re-deriving it.
func repairParents(steps []Step) {
	for i, phase := range phaseIDs(steps) {
		steps[i].ParentID = phase
	}
}

func repairDependencies(steps []Step) {
	index := make(map[string]bool, len(steps))
	for _, s := range steps {
		index[s.ID] = true
	}
	for i := range steps {
		kept := steps[i].DependsOn[:0]
		seen := make(map[string]bool, len(steps[i].DependsOn))
		for _, dep := range steps[i].DependsOn {
			if dep == steps[i].ID || !index[dep] || seen[dep] {
				continue
			}
			seen[dep] = true
			kept = append(kept, dep)
		}
		if len(kept) == 0 {
			kept = nil
		}
		steps[i].DependsOn = kept
	}
}

// Validate reports every defect a normalized plan can still carry, joined so a
// planner can fix them in one revision instead of one per round.
func (p Plan) Validate() error {
	var errs []error
	if strings.TrimSpace(p.Objective) == "" {
		errs = append(errs, errors.New("plan has no objective"))
	}
	if len(p.Steps) == 0 {
		errs = append(errs, errors.New("plan has no steps"))
	}
	if len(p.Steps) > MaxSteps {
		errs = append(errs, fmt.Errorf("plan has %d steps; the limit is %d", len(p.Steps), MaxSteps))
	}
	seen := make(map[string]bool, len(p.Steps))
	for _, s := range p.Steps {
		if strings.TrimSpace(s.Title) == "" {
			errs = append(errs, fmt.Errorf("step %q has no title", s.ID))
		}
		id := strings.TrimSpace(s.ID)
		if id == "" {
			errs = append(errs, fmt.Errorf("step %q has no id", s.Title))
			continue
		}
		if seen[id] {
			errs = append(errs, fmt.Errorf("step id %q is used more than once", id))
		}
		seen[id] = true
	}
	for _, group := range siblingGroups(p.Steps) {
		if _, cyclic := sortSiblings(group); cyclic {
			errs = append(errs, fmt.Errorf("steps %s form a dependency cycle", strings.Join(stepIDs(group), ", ")))
		}
	}
	return errors.Join(errs...)
}

func stepIDs(steps []Step) []string {
	out := make([]string, 0, len(steps))
	for _, s := range steps {
		out = append(out, s.ID)
	}
	return out
}

func cleanStrings(in []string) []string {
	out := make([]string, 0, len(in))
	seen := make(map[string]bool, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func cleanCriteria(in []Criterion) []Criterion {
	out := make([]Criterion, 0, len(in))
	for _, c := range in {
		text := strings.TrimSpace(c.Text)
		if text == "" {
			continue
		}
		out = append(out, Criterion{
			ID:         strings.TrimSpace(c.ID),
			Text:       text,
			Regression: c.Regression,
			Optional:   c.Optional,
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func cleanVerifications(in []Verification) []Verification {
	out := make([]Verification, 0, len(in))
	for _, v := range in {
		command := strings.TrimSpace(v.Command)
		expect := strings.TrimSpace(v.Expect)
		if command == "" && expect == "" {
			continue
		}
		out = append(out, Verification{Command: command, Expect: expect})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

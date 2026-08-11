package agent

import (
	"context"
	"sync"

	"reasonix/internal/plancontract"
)

// PlanSubmission is where submit_plan leaves an accepted plan for the host that
// armed the planning turn. It rides the context rather than the tool so one
// shared registry can serve concurrent turns with no mutable state between them,
// and so a call made outside a planning turn has nowhere to land.
type PlanSubmission struct {
	mu       sync.Mutex
	plan     plancontract.Plan
	previous plancontract.Plan
	attempts int
}

type planSubmissionKey struct{}

// WithPlanSubmission arms submit_plan for one planning turn and returns the
// submission the host reads once the planner finishes.
func WithPlanSubmission(ctx context.Context) (context.Context, *PlanSubmission) {
	s := &PlanSubmission{}
	return context.WithValue(ctx, planSubmissionKey{}, s), s
}

func planSubmissionFromContext(ctx context.Context) (*PlanSubmission, bool) {
	s, ok := ctx.Value(planSubmissionKey{}).(*PlanSubmission)
	return s, ok && s != nil
}

// Plan returns the submitted plan, or ok=false when the planner never submitted
// one and the host must fall back to reading its prose.
func (s *PlanSubmission) Plan() (plancontract.Plan, bool) {
	if s == nil {
		return plancontract.Plan{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.plan, s.attempts > 0
}

// record keeps the latest submission and stamps its revision. A planner that
// resubmits after fixing a validation error gets revision 2, so the count is the
// host's own record of how many tries the plan took.
func (s *PlanSubmission) record(p plancontract.Plan) plancontract.Plan {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.previous = s.plan
	s.attempts++
	p.Revision = s.attempts
	s.plan = p
	return p
}

// Revised compares a resubmission against the revision it replaces, so the
// planner is told what its own edit changed rather than assuming. ok is false
// for a first submission, which replaces nothing.
func (s *PlanSubmission) Revised() (plancontract.Diff, bool) {
	if s == nil {
		return plancontract.Diff{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.attempts < 2 {
		return plancontract.Diff{}, false
	}
	return plancontract.Compare(s.previous, s.plan), true
}

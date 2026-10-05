package control

import (
	"sort"

	"reasonix/internal/contract/event"
)

// Decisions are projections of host-owned state, not state themselves. Acting
// on one routes back to its owning subsystem and is accepted only if the
// decision identity is still current. Nothing in the kernel reads this view.

// DecisionKind names which subsystem owns a decision, and therefore which call
// answers it. A frontend may render them in one list; the host does not merge
// them, because "approve a workflow transition" and "tell me which option you
// want" are answered by different code with different rules.
type DecisionKind string

const (
	// DecisionPlanApproval is answered by ResolvePlanDecision.
	DecisionPlanApproval DecisionKind = "plan_approval"
	// DecisionToolApproval is answered by Approve.
	DecisionToolApproval DecisionKind = "tool_approval"
	// DecisionRecoveryApproval is answered by ResolveRecovery.
	DecisionRecoveryApproval DecisionKind = "recovery_approval"
	// DecisionAsk is answered by AnswerQuestion.
	DecisionAsk DecisionKind = "ask"
)

// Decision is one thing waiting on the user. ID is the identity its owner
// already issued; a frontend sends it back untouched and the owner resolves it,
// so a card answered after its subject moved on resolves nothing. The host does
// not expose the phase or epoch behind that identity — authority arithmetic is
// the kernel's, not the UI's.
type Decision struct {
	ID        string             `json:"id"`
	Kind      DecisionKind       `json:"kind"`
	Questions []DecisionQuestion `json:"questions,omitempty"`
}

// DecisionQuestion is one question in the shape the frontend already reads asks
// in. The event type has no json tags of its own, and a projection that spelled
// its fields differently from the event carrying the same question would make
// one renderer into two.
type DecisionQuestion struct {
	ID      string           `json:"id"`
	Header  string           `json:"header,omitempty"`
	Prompt  string           `json:"prompt"`
	Reason  string           `json:"reason,omitempty"`
	Options []DecisionOption `json:"options"`
	Multi   bool             `json:"multi,omitempty"`
}

// DecisionOption is one choice on a projected question.
type DecisionOption struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

func projectQuestions(qs []event.AskQuestion) []DecisionQuestion {
	out := make([]DecisionQuestion, len(qs))
	for i, q := range qs {
		opts := make([]DecisionOption, len(q.Options))
		for j, o := range q.Options {
			opts[j] = DecisionOption{Label: o.Label, Description: o.Description}
		}
		out[i] = DecisionQuestion{ID: q.ID, Header: q.Header, Prompt: q.Prompt, Reason: q.Reason, Options: opts, Multi: q.Multi}
	}
	return out
}

// Decisions snapshots what waits on the user, derived from the owning
// subsystems each time it is asked for, never the thing they derive from. It is
// the whole set: a frontend seals a prompt this list omits as answered
// elsewhere, so a kind left out wedges the run it blocks. Kind keeps the owners
// apart instead; the host serialises prompts, so today it holds at most one.
func (c *Controller) Decisions() []Decision {
	if c == nil {
		return nil
	}
	return c.approval.projectDecisions()
}

func (a *approvalManager) projectDecisions() []Decision {
	if a == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]Decision, 0, len(a.asks)+1)
	for id, pending := range a.approvals {
		out = append(out, Decision{ID: id, Kind: approvalDecisionKind(pending)})
	}
	for id, pending := range a.asks {
		// A queued ask is included: it is already blocking the turn, and this is
		// a snapshot a frontend pulls rather than an event it might replay. The
		// queued flag exists to stop replaying a question nobody ever saw.
		out = append(out, Decision{ID: id, Kind: DecisionAsk, Questions: projectQuestions(pending.questions)})
	}
	sortDecisions(out)
	return out
}

// approvalDecisionKind names the call that settles this approval. It reads the
// entry's shape rather than a label it carries: the recovery payload is what
// only ResolveRecovery can answer, and the plan gate is the one tool name the
// kernel issues for itself.
func approvalDecisionKind(p pendingApproval) DecisionKind {
	switch {
	case p.recovery != nil:
		return DecisionRecoveryApproval
	case p.tool == planApprovalTool:
		return DecisionPlanApproval
	default:
		return DecisionToolApproval
	}
}

// sortDecisions keeps the projection stable across repeated snapshots: Go map
// order is random, and a list that reshuffles every poll makes a frontend
// rebuild cards that never changed.
func sortDecisions(d []Decision) {
	sort.Slice(d, func(i, j int) bool { return d[i].ID < d[j].ID })
}

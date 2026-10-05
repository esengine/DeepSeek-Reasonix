package agent

import (
	"fmt"
	"slices"
	"strings"

	"reasonix/internal/runtime/completion"
	"reasonix/internal/runtime/contract"
	"reasonix/internal/runtime/verdict"
	"reasonix/internal/safety/evidence"
)

// planCriterionGaps answers unproven plan criteria from the sealed verdict, not
// the replayed contract a complete_step citation marks satisfied on the model's
// word: a criterion is proven only when every check its step names passed after
// the latest change, and a step naming none leaves it unproven, said as such.
// Without a sealed verdict the gaps stand as they were.
func (a *Agent) planCriterionGaps(gaps []completion.Gap) []completion.Gap {
	plan := a.PlanContract()
	res := a.turn.outcome
	if plan == nil || res == nil {
		return gaps
	}
	out := slices.DeleteFunc(slices.Clone(gaps), func(g completion.Gap) bool { return g.Kind == completion.GapUnprovenCriterion })
	if planChangesOf(plan) && !frozenSatisfied(res, "contract@"+contract.PlanDeliverable) {
		out = append(out, completion.Gap{Kind: completion.GapUnprovenCriterion, Detail: "plan: the plan names files to change, and no change was proven"})
	}
	for _, step := range plan.Steps {
		var failing []string
		for _, v := range step.Verification {
			id := evidence.VerificationIdentity(strings.TrimSpace(v.Command))
			if id != "" && !frozenSatisfied(res, "contract@command@"+id) {
				failing = append(failing, v.Command)
			}
		}
		for _, c := range step.Acceptance {
			if c.Optional {
				continue
			}
			switch {
			case len(step.Verification) == 0:
				out = append(out, completion.Gap{Kind: completion.GapUnprovenCriterion, Detail: fmt.Sprintf("%s: %s (no command checks it)", c.ID, c.Text)})
			case len(failing) > 0:
				out = append(out, completion.Gap{Kind: completion.GapUnprovenCriterion, Detail: fmt.Sprintf("%s: %s (not passed since the last change: %s)", c.ID, c.Text, strings.Join(failing, ", "))})
			}
		}
	}
	return out
}

func frozenSatisfied(res *verdict.Result, id string) bool {
	return slices.ContainsFunc(res.Obligations, func(o verdict.Obligation) bool {
		return o.ID == id && o.Verdict == verdict.Satisfied
	})
}

// The reasoning governor: while a turn sits in its exploration phase and the
// previous round bought expensive thinking, ride the next requests at reduced
// depth; stand down the moment the state stops matching. Fork-validated on
// frozen exploration states (pass parity exact, reasoning consistently down).
package agent

import (
	"os"

	"reasonix/internal/event"
	"reasonix/internal/evidence"
)

// governorEnabled gates enforcement for the A/B experiment; eligibility is
// always recorded so baseline arms carry the same shadow. Env-scoped on
// purpose — graduation to config waits on the experiment's verdict.
var governorEnabled = os.Getenv("REASONIX_EXPERIMENT_GOVERNOR") == "1"

// governorEffort is the reduced depth the engaged governor asks of the
// provider; endpoints whose vocabulary lacks it silently keep their default.
const governorEffort = "low"

type governorState struct {
	engaged bool
	noticed bool // the engage notice fires once per turn
}

// governorTrigger reports the exploration cell: no verification debt, no
// local execution yet, and the round that just ended bought expensive
// thinking. Shared verbatim with the fork-capture trigger so the live policy
// and its experiment freeze the same states.
func governorTrigger(sample evidence.OutcomeSample, lastReasoning int) bool {
	return sample.DebtAge == 0 && !sample.LocalExecSeen && lastReasoning >= govReasoningThreshold
}

// governorExit stands the override down: mutation debt opened, a local
// execution happened, or a discriminating observation landed — the turn has
// left exploration and full depth is worth buying again.
func governorExit(sample evidence.OutcomeSample) bool {
	return sample.DebtAge > 0 || sample.LocalExecSeen || sample.Discriminating > 0
}

// applyGovernor stamps eligibility on the round's sample and, under the
// experiment arm, toggles the per-request depth override.
func (a *Agent) applyGovernor(sample *evidence.OutcomeSample) {
	sample.GovernorEligible = governorTrigger(*sample, a.lastReasoning)
	if !governorEnabled {
		return
	}
	switch {
	case a.governor.engaged && governorExit(*sample):
		a.governor.engaged = false
	case !a.governor.engaged && sample.GovernorEligible:
		a.governor.engaged = true
		if !a.governor.noticed {
			a.governor.noticed = true
			a.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Code: event.NoticeCodeReasoningGovernor,
				Text:   "Exploration phase with expensive thinking; riding reduced reasoning depth until evidence work starts.",
				Detail: "reasoning governor engaged: no verification debt, no local execution, previous round over the reasoning threshold"})
		}
	}
	sample.GovernorEngaged = a.governor.engaged
}

// governorOverride is the request-scoped effort the engaged governor asks
// for; empty when disengaged so the configured depth stands.
func (a *Agent) governorOverride() string {
	if governorEnabled && a.governor.engaged {
		return governorEffort
	}
	return ""
}

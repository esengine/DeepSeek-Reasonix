package agent

import (
	"os"

	"reasonix/internal/event"
	"reasonix/internal/evidence"
)

// ebmBlindThreshold is the Evidence-Before-More-Mutation trigger: three
// mutations since the last discriminating observation. Healthy trajectories
// peak at two (edit, edit, check); the one recorded failure peaked at five.
const ebmBlindThreshold = 3

// ebmNudge deliberately permits finishing a coherent multi-file change first —
// the goal is discriminating feedback, not a mechanical test-after-every-edit.
const ebmNudge = "[evidence nudge] You have made several unverified mutations. Before making further " +
	"changes, obtain the cheapest discriminating evidence available for the current hypothesis. " +
	"If the workspace is not yet in a verifiable state, finish only the minimum coherent change " +
	"needed to make such a check possible."

// ebmEnabled gates enforcement for the A/B experiment; eligibility is always
// recorded so baseline arms carry the same shadow. Env-scoped on purpose —
// graduation to config waits on the experiment's verdict.
var ebmEnabled = os.Getenv("REASONIX_EXPERIMENT_EBM") == "1"

type ebmState struct {
	fired        bool
	captureArmed bool
	captured     bool
	captureRound int
}

// applyEBM stamps eligibility on the round's sample and, when the experiment
// arm is active, injects the nudge once per turn through the guard channel.
func (a *Agent) applyEBM(sample *evidence.OutcomeSample, results []string, outcomes []toolOutcome) {
	if sample.DebtAge == 0 || sample.BlindMutations < ebmBlindThreshold {
		return
	}
	sample.EBMEligible = true
	a.armForkCapture(*sample)
	if !ebmEnabled || a.ebm.fired || len(results) == 0 || len(outcomes) == 0 {
		return
	}
	a.ebm.fired = true
	a.ebm.captureArmed = false
	sample.EBMFired = true
	results[0] = outcomes[0].output + "\n\n" + ebmNudge
	a.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Code: event.NoticeCodeEvidenceNudge,
		Text:   "Several mutations are unverified; asking for the cheapest discriminating check.",
		Detail: "evidence nudge: verification debt open with blind mutations at threshold"})
}

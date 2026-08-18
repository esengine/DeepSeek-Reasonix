package main

import "fmt"

// outcomeSummary condenses one run's outcome-progress series: did claimed
// progress become objective transitions, and did the run end below its best
// verified state. Backfilled summaries derive from shell verification receipts
// when the recording predates the runtime shadow scorer.
type outcomeSummary struct {
	Rounds              int   `json:"rounds,omitempty"`
	ProgressRounds      int   `json:"progress_rounds,omitempty"`
	FalseProgressRounds int   `json:"false_progress_rounds,omitempty"`
	SolutionStallMax    int   `json:"solution_stall_max,omitempty"`
	Objective           int   `json:"objective,omitempty"`
	Regression          int   `json:"regression,omitempty"`
	BestScore           int   `json:"best_score,omitempty"`
	FinalScore          int   `json:"final_score"`
	RegressedFromBest   bool  `json:"regressed_from_best,omitempty"`
	SearchRegretMs      int64 `json:"search_regret_ms,omitempty"`
	// TTFDCMs is run start to the first discriminating observation (zero =
	// never); DebtAgeMax the worst stretch of rounds with an unverified mutation.
	TTFDCMs    int64 `json:"ttfdc_ms,omitempty"`
	DebtAgeMax int   `json:"debt_age_max,omitempty"`
	Backfilled bool  `json:"backfilled,omitempty"`

	// EBM chain: when the Evidence-Before-More-Mutation trigger held, when its
	// nudge fired, and what followed — compliance is the mechanism-health
	// readout that separates "nudge ignored" from "early evidence useless".
	DebtArea            int   `json:"debt_area,omitempty"`
	BlindPeak           int   `json:"blind_peak,omitempty"`
	EBMEligibleRound    int   `json:"ebm_eligible_round,omitempty"`
	EBMFiredRound       int   `json:"ebm_fired_round,omitempty"`
	EBMBlindAtFire      int   `json:"ebm_blind_at_fire,omitempty"`
	EBMDebtAgeAtFire    int   `json:"ebm_debt_age_at_fire,omitempty"`
	EBMRoundsToCheck    int   `json:"ebm_rounds_to_check,omitempty"`
	EBMMsToCheck        int64 `json:"ebm_ms_to_check,omitempty"`
	EBMReasoningToCheck int64 `json:"ebm_reasoning_to_check,omitempty"`
	EBMCheckWithin1     bool  `json:"ebm_check_within_1,omitempty"`
	EBMCheckWithin2     bool  `json:"ebm_check_within_2,omitempty"`
	// EBMExtraBlind counts mutations between the nudge and the first
	// discriminating check: 0 = strong compliance, 1 = finishing the minimum
	// coherent patch the copy permits, >=2 = real non-compliance.
	EBMExtraBlind   int    `json:"ebm_extra_blind_mutations"`
	EBMPostSequence string `json:"ebm_post_sequence,omitempty"`

	// Governor chain: rounds the exploration trigger held, rounds the depth
	// override actually rode requests, and where the first engagement sat.
	GovernorEligibleRounds int `json:"governor_eligible_rounds,omitempty"`
	GovernorEngagedRounds  int `json:"governor_engaged_rounds,omitempty"`
	GovernorFirstRound     int `json:"governor_first_round,omitempty"`

	// Runway shadow fields are absent for trajectories recorded before the
	// experiment. FirstSpentRound is the counterfactual intervention point.
	RunwaySamples         int `json:"runway_samples,omitempty"`
	RunwayMin             int `json:"runway_min,omitempty"`
	RunwayFinal           int `json:"runway_final,omitempty"`
	RunwayDryMax          int `json:"runway_dry_max,omitempty"`
	RunwayIdleMax         int `json:"runway_idle_max,omitempty"`
	RunwayFirstSpentRound int `json:"runway_first_spent_round,omitempty"`
}

// outcomePoint is one recorded shadow sample plus its observation time.
type outcomePoint struct {
	ts                                                      int64
	round                                                   int
	exploration, verification, objective, regression, churn int
	legacyGain, discriminating, debtAge, blindMutations     int
	ebmEligible, ebmFired                                   bool
	governorEligible, governorEngaged                       bool
	runway                                                  *int
	runwayDry, runwayIdle                                   int
	runwaySpent                                             bool
}

// verifyPoint is one backfilled verification-transition observation.
type verifyPoint struct {
	ts                    int64
	objective, regression int
}

// falseProgressWindow bounds how many later rounds may redeem a legacy
// progress claim with an objective transition before the round counts false.
const falseProgressWindow = 3

// observeVerification folds one verification-classified shell result into the
// backfill series; key identity is the exact (name, args) announcement.
func (t *trajScan) observeVerification(key string, passed bool, ts int64) {
	if t.verifyPass == nil {
		t.verifySeen = map[string]bool{}
		t.verifyPass = map[string]bool{}
	}
	seen, was := t.verifySeen[key], t.verifyPass[key]
	t.verifySeen[key] = true
	t.verifyPass[key] = passed
	p := verifyPoint{ts: ts}
	if seen && passed && !was {
		p.objective = 1
	}
	if seen && !passed && was {
		p.regression = 1
	}
	t.verifyPoints = append(t.verifyPoints, p)
}

// summarizeOutcome prefers recorded shadow samples; older recordings fall back
// to the verification backfill, which cannot price legacy-scorer claims.
func (t *trajScan) summarizeOutcome() *outcomeSummary {
	if len(t.outcomePoints) > 0 {
		o := summarizeOutcomePoints(t.outcomePoints, t.firstTS, t.lastTS)
		t.attachEBMChain(o)
		return o
	}
	if len(t.verifyPoints) > 0 {
		return summarizeVerifyBackfill(t.verifyPoints, t.lastTS)
	}
	return nil
}

func summarizeOutcomePoints(points []outcomePoint, firstTS, lastTS int64) *outcomeSummary {
	o := &outcomeSummary{Rounds: len(points)}
	verifying, solution, stall := false, false, 0
	score, best := 0, 0
	var bestTS int64
	for i, p := range points {
		if p.verification > 0 {
			verifying = true
		}
		if p.discriminating > 0 && o.TTFDCMs == 0 && p.ts > firstTS {
			o.TTFDCMs = p.ts - firstTS
		}
		o.DebtAgeMax = max(o.DebtAgeMax, p.debtAge)
		o.Objective += p.objective
		o.Regression += p.regression
		if p.legacyGain > 0 {
			o.ProgressRounds++
		}
		if p.runway != nil {
			if o.RunwaySamples == 0 {
				o.RunwayMin = *p.runway
			}
			o.RunwaySamples++
			o.RunwayMin = min(o.RunwayMin, *p.runway)
			o.RunwayFinal = *p.runway
			o.RunwayDryMax = max(o.RunwayDryMax, p.runwayDry)
			o.RunwayIdleMax = max(o.RunwayIdleMax, p.runwayIdle)
			if o.RunwayFirstSpentRound == 0 && p.runwaySpent {
				o.RunwayFirstSpentRound = p.round
				if o.RunwayFirstSpentRound == 0 {
					o.RunwayFirstSpentRound = i + 1
				}
			}
		}
		// The solution stall clock only starts once the run enters its solution
		// phase (a verification attempt or a mutation); pure research runs with
		// no verification would otherwise read as one long stall.
		if p.verification > 0 || p.churn > 0 {
			solution = true
		}
		if solution {
			if p.objective > 0 {
				stall = 0
			} else {
				stall++
				o.SolutionStallMax = max(o.SolutionStallMax, stall)
			}
		}
		score += p.objective - p.regression
		if score > best {
			best, bestTS = score, p.ts
		}
	}
	if verifying {
		for i, p := range points {
			if p.legacyGain <= 0 {
				continue
			}
			redeemed := false
			for j := i; j < min(i+1+falseProgressWindow, len(points)); j++ {
				if points[j].objective > 0 {
					redeemed = true
					break
				}
			}
			if !redeemed {
				o.FalseProgressRounds++
			}
		}
	}
	finishScore(o, score, best, bestTS, lastTS)
	return o
}

// attachEBMChain condenses the per-round EBM shadow into the run's chain
// facts; rounds-to-check joins the cognition digests for the tokens spent
// between fire and first discriminating observation.
func (t *trajScan) attachEBMChain(o *outcomeSummary) {
	pts := t.outcomePoints
	fire := -1
	for i, p := range pts {
		o.DebtArea += p.debtAge
		o.BlindPeak = max(o.BlindPeak, p.blindMutations)
		if o.EBMEligibleRound == 0 && p.ebmEligible {
			o.EBMEligibleRound = i + 1
		}
		if p.governorEligible {
			o.GovernorEligibleRounds++
		}
		if p.governorEngaged {
			o.GovernorEngagedRounds++
			if o.GovernorFirstRound == 0 {
				o.GovernorFirstRound = i + 1
			}
		}
		if fire < 0 && p.ebmFired {
			fire = i
		}
	}
	if fire < 0 {
		return
	}
	o.EBMFiredRound = fire + 1
	o.EBMBlindAtFire = pts[fire].blindMutations
	o.EBMDebtAgeAtFire = pts[fire].debtAge
	for j := fire + 1; j < len(pts); j++ {
		o.EBMPostSequence += postEBMCategory(pts[j])
		if pts[j].discriminating == 0 {
			o.EBMExtraBlind += pts[j].churn
			continue
		}
		o.EBMRoundsToCheck = j - fire
		o.EBMMsToCheck = pts[j].ts - pts[fire].ts
		o.EBMCheckWithin1 = j-fire <= 1
		o.EBMCheckWithin2 = j-fire <= 2
		for k := fire + 1; k <= j && k < len(t.s.Rounds); k++ {
			o.EBMReasoningToCheck += t.s.Rounds[k].ReasoningTokens
		}
		return
	}
}

// postEBMCategory letters the rounds after a nudge: V discriminating check,
// M mutation, R new information, "." quiet.
func postEBMCategory(p outcomePoint) string {
	switch {
	case p.discriminating > 0:
		return "V"
	case p.churn > 0:
		return "M"
	case p.exploration > 0:
		return "R"
	default:
		return "."
	}
}

// governorShadowLine aggregates the reasoning-governor shadow across runs;
// empty when no round was eligible.
func governorShadowLine(results []result) string {
	runs, elig, engaged := 0, 0, 0
	for _, r := range results {
		if r.Trajectory == nil || r.Trajectory.Outcome == nil {
			continue
		}
		o := r.Trajectory.Outcome
		if o.GovernorEligibleRounds > 0 {
			runs++
		}
		elig += o.GovernorEligibleRounds
		engaged += o.GovernorEngagedRounds
	}
	if elig == 0 {
		return ""
	}
	return fmt.Sprintf(" · **governor** eligible %d rounds in %d runs · engaged %d rounds", elig, runs, engaged)
}

func summarizeVerifyBackfill(points []verifyPoint, lastTS int64) *outcomeSummary {
	o := &outcomeSummary{Backfilled: true}
	score, best := 0, 0
	var bestTS int64
	for _, p := range points {
		o.Objective += p.objective
		o.Regression += p.regression
		score += p.objective - p.regression
		if score > best {
			best, bestTS = score, p.ts
		}
	}
	finishScore(o, score, best, bestTS, lastTS)
	return o
}

func finishScore(o *outcomeSummary, score, best int, bestTS, lastTS int64) {
	o.BestScore, o.FinalScore = best, score
	if score < best {
		o.RegressedFromBest = true
		if lastTS > bestTS {
			o.SearchRegretMs = lastTS - bestTS
		}
	}
}

// renderOutcomeProgress aggregates the shadow scorer's verdicts: how often the
// live novelty scorer claimed progress that never became an objective
// transition, and how many runs peaked above their final verified state.
func renderOutcomeProgress(results []result) string {
	runs, backfilled := 0, 0
	progress, falseProgress := 0, 0
	objective, regression, regressed := 0, 0, 0
	var regretMs int64
	stallMax := 0
	for _, r := range results {
		if r.Trajectory == nil || r.Trajectory.Outcome == nil {
			continue
		}
		o := r.Trajectory.Outcome
		runs++
		if o.Backfilled {
			backfilled++
		}
		progress += o.ProgressRounds
		falseProgress += o.FalseProgressRounds
		objective += o.Objective
		regression += o.Regression
		stallMax = max(stallMax, o.SolutionStallMax)
		if o.RegressedFromBest {
			regressed++
			regretMs += o.SearchRegretMs
		}
	}
	if runs == 0 {
		return ""
	}
	discRuns, debtMax := 0, 0
	var ttfdcs []int64
	for _, r := range results {
		if r.Trajectory == nil || r.Trajectory.Outcome == nil {
			continue
		}
		o := r.Trajectory.Outcome
		debtMax = max(debtMax, o.DebtAgeMax)
		if o.TTFDCMs > 0 {
			discRuns++
			ttfdcs = append(ttfdcs, o.TTFDCMs)
		}
	}
	line := fmt.Sprintf("**Outcome shadow** (%d runs): **objective transitions** %d · **regressions** %d · **regressed from best** %d (%s)",
		runs, objective, regression, regressed, pct(regressed, runs))
	if discRuns > 0 {
		line += fmt.Sprintf(" · **discriminating checks** in %d/%d runs (TTFDC p50 %s)",
			discRuns, runs, dur(median(ttfdcs)))
	}
	if debtMax > 0 {
		line += fmt.Sprintf(" · **verification debt max** %d rounds", debtMax)
	}
	elig, fired, comply := 0, 0, 0
	var toCheck []int64
	for _, r := range results {
		if r.Trajectory == nil || r.Trajectory.Outcome == nil {
			continue
		}
		o := r.Trajectory.Outcome
		if o.EBMEligibleRound > 0 {
			elig++
		}
		if o.EBMFiredRound > 0 {
			fired++
			// Compliance judges behavior, not speed: at most one mutation
			// between nudge and check — the coherent-patch allowance.
			if o.EBMRoundsToCheck > 0 && o.EBMExtraBlind <= 1 {
				comply++
			}
			if o.EBMRoundsToCheck > 0 {
				toCheck = append(toCheck, int64(o.EBMRoundsToCheck))
			}
		}
	}
	if elig > 0 {
		line += fmt.Sprintf(" · **EBM** eligible %d · fired %d", elig, fired)
		if fired > 0 {
			line += fmt.Sprintf(" (compliance ≤1 extra mutation %s, median rounds-to-check %d)",
				pct(comply, fired), median(toCheck))
		}
	}
	line += governorShadowLine(results)
	line += runwayShadowLine(results)
	if progress > 0 {
		line += fmt.Sprintf(" · **false progress** %d/%d (%s)", falseProgress, progress, pct(falseProgress, progress))
	}
	if stallMax > 0 {
		line += fmt.Sprintf(" · **solution stall max** %d rounds", stallMax)
	}
	if regressed > 0 {
		line += fmt.Sprintf(" · **avg search regret** %s", dur(regretMs/int64(regressed)))
	}
	if backfilled > 0 {
		line += fmt.Sprintf(" · backfilled %d", backfilled)
	}
	return line + "\n\n"
}

func runwayShadowLine(results []result) string {
	measured, spent, dryMax, idleMax := 0, 0, 0, 0
	var spentRounds []int64
	for _, r := range results {
		if r.Trajectory == nil || r.Trajectory.Outcome == nil || r.Trajectory.Outcome.RunwaySamples == 0 {
			continue
		}
		o := r.Trajectory.Outcome
		measured++
		dryMax = max(dryMax, o.RunwayDryMax)
		idleMax = max(idleMax, o.RunwayIdleMax)
		if o.RunwayFirstSpentRound > 0 {
			spent++
			spentRounds = append(spentRounds, int64(o.RunwayFirstSpentRound))
		}
	}
	if measured == 0 {
		return ""
	}
	line := fmt.Sprintf(" · **runway shadow** would spend in %d/%d runs (%s)", spent, measured, pct(spent, measured))
	if spent > 0 {
		line += fmt.Sprintf(" at median round %d", median(spentRounds))
	}
	return fmt.Sprintf("%s · max dry/idle %d/%d", line, dryMax, idleMax)
}

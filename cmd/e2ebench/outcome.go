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
	// VerificationRevisits is how many checks ran a second time. At zero no
	// transition was observable, so a false-progress ratio would be measuring
	// its own blindness.
	VerificationRevisits int `json:"verification_revisits,omitempty"`

	// DebtArea is the run's total unverified-mutation exposure (debt age summed
	// over rounds); BlindPeak the worst run of mutations with no check between.
	DebtArea  int `json:"debt_area,omitempty"`
	BlindPeak int `json:"blind_peak,omitempty"`
}

// outcomePoint is one recorded shadow sample plus its observation time.
type outcomePoint struct {
	ts                                                      int64
	exploration, verification, objective, regression, churn int
	legacyGain, discriminating, debtAge, blindMutations     int
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
	if seen {
		t.verifyRevisits++
	}
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
	var o *outcomeSummary
	switch {
	case len(t.outcomePoints) > 0:
		o = summarizeOutcomePoints(t.outcomePoints, t.firstTS, t.lastTS)
		t.attachDebtExposure(o)
	case len(t.verifyPoints) > 0:
		o = summarizeVerifyBackfill(t.verifyPoints, t.lastTS)
	default:
		return nil
	}
	o.VerificationRevisits = t.verifyRevisits
	return o
}

func summarizeOutcomePoints(points []outcomePoint, firstTS, lastTS int64) *outcomeSummary {
	o := &outcomeSummary{Rounds: len(points)}
	verifying, solution, stall := false, false, 0
	score, best := 0, 0
	var bestTS int64
	for _, p := range points {
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

// attachDebtExposure sums the run's unverified-mutation exposure: how long
// debt stayed open and how many mutations ever stacked up without a check.
func (t *trajScan) attachDebtExposure(o *outcomeSummary) {
	for _, p := range t.outcomePoints {
		o.DebtArea += p.debtAge
		o.BlindPeak = max(o.BlindPeak, p.blindMutations)
	}
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
	objective, regression, regressed, revisits := 0, 0, 0, 0
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
		revisits += o.VerificationRevisits
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
	if progress > 0 {
		if revisits == 0 {
			line += " · **false progress** unmeasured (no check ran twice)"
		} else {
			line += fmt.Sprintf(" · **false progress** %d/%d (%s)", falseProgress, progress, pct(falseProgress, progress))
		}
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

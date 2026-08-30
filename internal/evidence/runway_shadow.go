package evidence

// The runway shadow prices one turn's investigation without changing any
// runtime decision. Every round costs the same; observable outcomes buy some
// or all of that cost back. Keeping the account private to OutcomeTracker makes
// the experiment telemetry-only until recorded data justifies a policy change.
const (
	runwayRoundCost        = 4
	runwayYieldFalsifiable = 3 * runwayRoundCost
	runwayYieldChange      = runwayRoundCost
	runwayYieldExploration = runwayRoundCost - 1

	// In exploration-rate units, a fresh account covers 24 productive reads and
	// a fully banked one covers 40. A round producing nothing burns four units.
	runwayStartBalance = 24
	runwayMaxBalance   = 40
)

type runwayShadow struct {
	balance  int
	observed bool
	dry      int
	idle     int
}

type runwayShadowState struct {
	balance int
	dry     int
	idle    int
	spent   bool
}

func (r *runwayShadow) observe(s OutcomeSample) runwayShadowState {
	if !r.observed {
		r.balance, r.observed = runwayStartBalance, true
	}
	yield := runwayYield(s)
	wasSolvent := r.balance > 0
	r.balance = min(max(r.balance+yield-runwayRoundCost, 0), runwayMaxBalance)

	if s.Discriminating > 0 || s.Objective > 0 || s.Churn > 0 {
		r.idle = 0
	} else {
		r.idle++
	}
	if yield > 0 {
		r.dry = 0
	} else {
		r.dry++
	}
	return runwayShadowState{
		balance: r.balance,
		dry:     r.dry,
		idle:    r.idle,
		spent:   wasSolvent && r.balance == 0,
	}
}

func runwayYield(s OutcomeSample) int {
	switch {
	case s.Discriminating > 0 || s.Objective > 0:
		return runwayYieldFalsifiable
	case s.Churn > 0:
		return runwayYieldChange
	case s.Exploration > 0:
		return runwayYieldExploration
	default:
		return 0
	}
}

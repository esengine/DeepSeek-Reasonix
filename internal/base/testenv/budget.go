package testenv

import "time"

// Deadliner is the part of *testing.T a budget is read from. It is separate
// from TB so a caller that only wants a temp dir is not asked to supply it.
type Deadliner interface {
	Deadline() (time.Time, bool)
}

// budgetCeiling caps a wait that has no deadline to read. Long enough that a
// loaded runner never reaches it by being slow, short enough that a genuine
// hang is reported rather than left to the whole binary's timeout.
const budgetCeiling = 30 * time.Second

// Budget bounds a wait whose only job is to catch a hang, never to assert
// latency: a fixed two seconds fails whenever the tree is tested at once, and
// reports a hang that did not happen on a runner that was merely busy. Half of
// the binary's remaining deadline keeps -timeout the single knob and still
// leaves room to report the failure before the binary dies.
func Budget(t Deadliner) time.Duration {
	deadline, ok := t.Deadline()
	if !ok {
		return budgetCeiling
	}
	if half := time.Until(deadline) / 2; half > 0 && half < budgetCeiling {
		return half
	}
	return budgetCeiling
}

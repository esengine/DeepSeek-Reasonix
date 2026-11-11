package schedule

import "reasonix/internal/contract/process"

// ProcessClass projects the run state onto the shared process vocabulary.
func (s RunState) ProcessClass() (process.Class, bool) {
	switch s {
	case RunClaimed:
		return process.Class{State: process.Queued}, true
	case RunRunning:
		return process.Class{State: process.Running}, true
	case RunSucceeded:
		return process.Class{State: process.Completed}, true
	case RunFailed:
		return process.Class{State: process.Failed}, true
	case RunBlocked:
		return process.Class{State: process.Stopped, Cause: process.CauseParked}, true
	case RunBudgetStopped:
		return process.Class{State: process.Stopped, Cause: process.CauseBudget}, true
	case RunInterrupted:
		return process.Class{State: process.Interrupted}, true
	case RunSkipped:
		return process.Class{State: process.Skipped}, true
	}
	return process.Class{}, false
}

package agentgraph

import "reasonix/internal/contract/process"

// ProcessClass projects the node state onto the shared process vocabulary. A
// queued node carries the cause the scheduler held it for.
func (s NodeState) ProcessClass(w WaitCause) (process.Class, bool) {
	switch s {
	case StatePending:
		return process.Class{State: process.Pending}, true
	case StateQueued:
		return process.Class{State: process.Queued, Cause: waitCause(w)}, true
	case StateRunning:
		return process.Class{State: process.Running}, true
	case StateCompleted:
		return process.Class{State: process.Completed}, true
	case StateAdopted:
		return process.Class{State: process.Completed, Cause: process.CauseAdopted}, true
	case StateFailed:
		return process.Class{State: process.Failed}, true
	case StateCancelled:
		return process.Class{State: process.Stopped}, true
	case StateSkipped:
		return process.Class{State: process.Skipped}, true
	}
	return process.Class{}, false
}

func waitCause(w WaitCause) process.Cause {
	switch w {
	case WaitSlots:
		return process.CauseSlots
	case WaitWriters:
		return process.CauseWriters
	case WaitClaim:
		return process.CauseClaim
	}
	return process.CauseNone
}

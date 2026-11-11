package sessioninbox

import "reasonix/internal/contract/process"

// ProcessClass projects the item state onto the shared process vocabulary.
func (s InboxState) ProcessClass() (process.Class, bool) {
	switch s {
	case StateQueued:
		return process.Class{State: process.Queued}, true
	case StateBlocked:
		return process.Class{State: process.Queued, Cause: process.CauseHeld}, true
	case StateRunning, StateSteerAccepted:
		return process.Class{State: process.Running}, true
	case StateSteerConsumed:
		return process.Class{State: process.Completed}, true
	case StateUncertain:
		return process.Class{State: process.Interrupted}, true
	}
	return process.Class{}, false
}

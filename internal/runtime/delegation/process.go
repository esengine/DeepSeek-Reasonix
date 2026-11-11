package delegation

import "reasonix/internal/contract/process"

// ProcessClass projects the run state onto the shared process vocabulary.
func (s RunState) ProcessClass() (process.Class, bool) {
	switch s {
	case RunRunning:
		return process.Class{State: process.Running}, true
	case RunCompleted:
		return process.Class{State: process.Completed}, true
	case RunFailed:
		return process.Class{State: process.Failed}, true
	case RunCancelled:
		return process.Class{State: process.Stopped}, true
	case RunInterrupted:
		return process.Class{State: process.Interrupted}, true
	}
	return process.Class{}, false
}

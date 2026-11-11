package taskmonitor

import "reasonix/internal/contract/process"

// ProcessClass projects the task state onto the shared process vocabulary.
func (s TaskState) ProcessClass() (process.Class, bool) {
	switch s {
	case TaskStateQueued:
		return process.Class{State: process.Queued}, true
	case TaskStateRunning:
		return process.Class{State: process.Running}, true
	case TaskStateWaiting:
		return process.Class{State: process.Waiting}, true
	case TaskStateSucceeded:
		return process.Class{State: process.Completed}, true
	case TaskStateFailed:
		return process.Class{State: process.Failed}, true
	case TaskStateCancelled:
		return process.Class{State: process.Stopped}, true
	case TaskStateStale:
		return process.Class{State: process.Interrupted}, true
	}
	return process.Class{}, false
}

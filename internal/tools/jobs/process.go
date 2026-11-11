package jobs

import "reasonix/internal/contract/process"

// ProcessClass projects the job state onto the shared process vocabulary.
func (s Status) ProcessClass() (process.Class, bool) {
	switch s {
	case Running:
		return process.Class{State: process.Running}, true
	case Done:
		return process.Class{State: process.Completed}, true
	case Failed:
		return process.Class{State: process.Failed}, true
	case Killed:
		return process.Class{State: process.Stopped}, true
	case Interrupted:
		return process.Class{State: process.Interrupted}, true
	}
	return process.Class{}, false
}

package jobs

// Status is a job's lifecycle state.
type Status string

const (
	Running     Status = "running"
	Done        Status = "done"
	Failed      Status = "failed"
	Killed      Status = "killed"
	Interrupted Status = "interrupted"
)

// Cause says why a job reached a status nobody asked for, or "" when the
// status speaks for itself. Only the host knows the session ended under it.
func (s Status) Cause() string {
	if s == Interrupted {
		return " — the session that ran it ended before it finished (a restart or close), not a kill; its output up to then is kept"
	}
	return ""
}

// cancelledStatus is what a cancelled job ended as: Interrupted when the
// manager itself is closing under it, Killed when only the job was stopped.
func (m *Manager) cancelledStatus() Status {
	if m.root.Err() != nil {
		return Interrupted
	}
	return Killed
}

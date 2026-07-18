package control

// JobCancellation is the focused optional port used by runtime hosts. It is
// intentionally not added to SessionAPI while other frontends migrate: a Host
// must assert this port before admitting job/cancel and return
// CAPABILITY_UNAVAILABLE for a legacy/test controller that lacks it.
type JobCancellation interface {
	CancelBackgroundJob(jobID string) bool
}

// CancelBackgroundJob delegates to the exact session-scoped jobs.Manager
// primitive. Unknown, completed, and already-cancelled IDs all return false;
// callers expose that as the successful not_running disposition.
func (c *Controller) CancelBackgroundJob(jobID string) bool {
	if c == nil || c.jobs == nil {
		return false
	}
	return c.jobs.KillForSession(c.parentSessionID(), jobID)
}

var _ JobCancellation = (*Controller)(nil)

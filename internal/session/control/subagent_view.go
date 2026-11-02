package control

import (
	"reasonix/internal/runtime/delegation"
)

// SubagentReader is what the controller asks of the sub-agent store: the runs a
// conversation spawned, and one run's conversation, live or stored.
type SubagentReader interface {
	Runs(parentSession, workspaceRoot string) ([]delegation.RunInfo, error)
	Transcript(ref, parentSession, workspaceRoot string, after, limit int) (delegation.RunTranscript, error)
}

// SubagentRuns lists the sub-agent runs this session spawned, newest first. A
// session with no file of its own has spawned none that could be found again.
func (c *Controller) SubagentRuns() ([]delegation.RunInfo, error) {
	parent := c.parentSessionID()
	if c.subagents == nil || parent == "" {
		return nil, nil
	}
	return c.subagents.Runs(parent, c.WorkspaceRoot())
}

// SubagentTranscript reads one of this session's sub-agent runs from message
// index after, a page at a time. A run of another conversation is refused.
func (c *Controller) SubagentTranscript(ref string, after, limit int) (delegation.RunTranscript, error) {
	parent := c.parentSessionID()
	if c.subagents == nil || parent == "" {
		return delegation.RunTranscript{}, delegation.ErrSubagentNotFound
	}
	return c.subagents.Transcript(ref, parent, c.WorkspaceRoot(), after, limit)
}

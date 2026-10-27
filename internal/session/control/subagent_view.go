package control

import (
	"reasonix/internal/runtime/delegation"
)

// SubagentReader is what the controller asks of the sub-agent store: the runs a
// conversation spawned, and one run's conversation, live or stored.
type SubagentReader interface {
	Runs(parentSession, workspaceRoot string) ([]delegation.RunInfo, error)
	Transcript(ref, parentSession, workspaceRoot string, after, limit int) (delegation.RunTranscript, error)
	Steer(ref, parentSession, workspaceRoot, text string, from delegation.SteerSource, handoff ...delegation.GuidanceContext) error
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

// SteerSubagent sends the person's guidance to one of this session's running
// sub-agents, to be read at its next tool boundary. The limits on size and rate
// are the user's own settings, and a run that is not inside a turn refuses.
func (c *Controller) SteerSubagent(ref, text string, handoff ...delegation.GuidanceContext) error {
	parent := c.parentSessionID()
	if c.subagents == nil || parent == "" {
		return delegation.ErrSubagentNotFound
	}
	return c.subagents.Steer(ref, parent, c.WorkspaceRoot(), text, delegation.SteerFromUser, handoff...)
}

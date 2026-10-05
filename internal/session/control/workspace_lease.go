// workspace_lease.go — what a controller says about the workspace write lease.
package control

import "reasonix/internal/state/workspacelease"

// WorkspaceLeaseState reports only whether this controller owns or is waiting
// for the Delivery workspace writer lease. It never exposes filesystem or
// process identity.
func (c *Controller) WorkspaceLeaseState() workspacelease.State {
	return c.workspaceLease.State()
}

// NameWorkspaceHolder gives this session a name for another session to see
// while waiting on the workspace this one is writing.
func (c *Controller) NameWorkspaceHolder(name func() string) {
	c.workspaceLease.SetHolder(name)
}

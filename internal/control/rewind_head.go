package control

import (
	"log/slog"

	"reasonix/internal/agent"
)

// undoHeadRewind returns the controller to the head it left through an
// in-place conversation rewind, provided nothing was added on the rewind head
// since: the empty rewind head is retired and its parent becomes current. A
// rewind head that was continued stays a version and undo leaves it alone.
func (c *Controller) undoHeadRewind() bool {
	sess := c.headBranchSession()
	if sess == nil {
		return false
	}
	ref, ok := sess.Head()
	if !ok {
		return false
	}
	path := c.SessionPath()
	heads, err := agent.ListSessionHeads(path)
	if err != nil {
		return false
	}
	var current *agent.SessionHead
	for i := range heads {
		if heads[i].ID == ref.HeadID {
			current = &heads[i]
		}
	}
	if current == nil || current.Kind != agent.HeadKindRewind || current.LeafID != current.ForkFrom || current.ParentHead == "" {
		return false
	}
	c.snapshotMu.Lock()
	defer c.snapshotMu.Unlock()
	if err := sess.SwitchHead(path, current.ParentHead); err != nil {
		slog.Warn("controller: undo could not return to the parent head", "path", path, "head", current.ParentHead, "err", err)
		return false
	}
	c.afterHeadSwitch(path)
	if err := agent.RetireSessionHead(path, current.ID); err != nil {
		slog.Warn("controller: undo left the empty rewind head in place", "path", path, "head", current.ID, "err", err)
	}
	return true
}

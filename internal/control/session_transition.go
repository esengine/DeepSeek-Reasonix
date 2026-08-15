package control

import (
	"fmt"
	"strings"

	"reasonix/internal/agent"
)

// SessionTransitionInfo describes an intentional controller path change. The
// candidate stays private; its owner binds it through BindWriteAuthority before
// the controller publishes it as current.
type SessionTransitionInfo struct {
	OriginalPath string
	TargetPath   string
	Reason       string

	session *agent.Session
}

// BindWriteAuthority binds the transition candidate to lease.
func (i SessionTransitionInfo) BindWriteAuthority(lease *agent.SessionLease) error {
	if i.session == nil {
		return fmt.Errorf("session transition candidate is unavailable")
	}
	if lease == nil {
		i.session.RequireWriteAuthority()
		i.session.ClearWriteAuthority()
		return agent.ErrSessionWriteAuthorityMissing
	}
	if lease.Path() != agent.CanonicalSessionPath(i.TargetPath) {
		return fmt.Errorf("session transition lease does not cover target")
	}
	return lease.Writer().Bind(i.session, agent.NextSessionWriteGeneration())
}

// SetOnSessionTransition installs the owner handoff used before a path change.
func (c *Controller) SetOnSessionTransition(fn func(SessionTransitionInfo) error) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.onSessionTransition = fn
	c.mu.Unlock()
}

func (c *Controller) sessionTransitionHandler() func(SessionTransitionInfo) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.onSessionTransition
}

func (c *Controller) prepareSessionTransition(targetPath, reason string, candidate *agent.Session) error {
	targetPath = strings.TrimSpace(targetPath)
	if targetPath == "" || candidate == nil {
		return fmt.Errorf("session transition target is unavailable")
	}
	handler := c.sessionTransitionHandler()
	if handler == nil {
		// Embedded/test controllers that never required a writer retain their
		// permissive behavior. A writer-bound controller must fail closed.
		if current := c.executor.Session(); current != nil && current.WriteAuthorityRequired() {
			candidate.RequireWriteAuthority()
			return agent.ErrSessionWriteAuthorityMissing
		}
		return nil
	}
	info := SessionTransitionInfo{
		OriginalPath: c.SessionPath(),
		TargetPath:   targetPath,
		Reason:       reason,
		session:      candidate,
	}
	if err := handler(info); err != nil {
		return err
	}
	auth := candidate.WriteAuthority()
	if candidate.WriteAuthorityRequired() && (auth == nil || !auth.Covers(targetPath)) {
		return agent.ErrSessionWriteAuthorityMissing
	}
	return nil
}

package control

import (
	"fmt"
	"strings"

	"reasonix/internal/agent"
	"reasonix/internal/guardian"
)

// SessionTransitionInfo describes an intentional controller path change. The
// candidate stays private; its owner binds it through BindWriteAuthority before
// the controller publishes it as current.
type SessionTransitionInfo struct {
	OriginalPath string
	TargetPath   string
	Reason       string

	session *agent.Session
	commit  *sessionTransitionCommit
}

type sessionTransitionCommit struct {
	controller *Controller
	targetPath string
	session    *agent.Session
	hooks      []func()
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

// OnCommit defers publication work until the Controller has committed both its
// session path and executor Session to the transition target.
func (i SessionTransitionInfo) OnCommit(fn func()) {
	if i.commit == nil || fn == nil {
		return
	}
	i.commit.hooks = append(i.commit.hooks, fn)
}

func (c *sessionTransitionCommit) publish() {
	if c == nil {
		return
	}
	c.controller.mu.Lock()
	c.controller.sessionPath = c.targetPath
	c.controller.guardianPath = guardian.PathFor(c.targetPath)
	c.controller.mu.Unlock()
	c.controller.setActiveJobSession(c.targetPath)
	if c.controller.executor != nil {
		c.controller.executor.SetSession(c.session)
	}
	for _, fn := range c.hooks {
		fn()
	}
	c.hooks = nil
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

func (c *Controller) prepareSessionTransition(targetPath, reason string, candidate *agent.Session) (*sessionTransitionCommit, error) {
	targetPath = strings.TrimSpace(targetPath)
	if candidate == nil {
		return nil, fmt.Errorf("session transition target is unavailable")
	}
	commit := &sessionTransitionCommit{controller: c, targetPath: targetPath, session: candidate}
	// In-memory controllers intentionally rotate Sessions without persistence.
	// They have no lease or routable path to transfer, but still need the same
	// atomic executor swap used by persisted controllers.
	if targetPath == "" {
		return commit, nil
	}
	handler := c.sessionTransitionHandler()
	if handler == nil {
		// Embedded/test controllers that never required a writer retain their
		// permissive behavior. A writer-bound controller must fail closed.
		if current := c.executor.Session(); current != nil && current.WriteAuthorityRequired() {
			candidate.RequireWriteAuthority()
			return nil, agent.ErrSessionWriteAuthorityMissing
		}
		return commit, nil
	}
	info := SessionTransitionInfo{
		OriginalPath: c.SessionPath(),
		TargetPath:   targetPath,
		Reason:       reason,
		session:      candidate,
		commit:       commit,
	}
	if err := handler(info); err != nil {
		return nil, err
	}
	auth := candidate.WriteAuthority()
	if candidate.WriteAuthorityRequired() && (auth == nil || !auth.Covers(targetPath)) {
		return nil, agent.ErrSessionWriteAuthorityMissing
	}
	return commit, nil
}

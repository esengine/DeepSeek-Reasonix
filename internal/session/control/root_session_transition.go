package control

import (
	"context"

	"reasonix/internal/state/sessionstore"
)

// ConfigureSessionLeaseAdoption lets a host accept a lease already acquired by
// the session transition owner. It must retain the old owner on failure.
func (c *Controller) ConfigureSessionLeaseAdoption(adopt func(*sessionstore.SessionLease) error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.composition == nil {
		c.composition = &compositionControl{}
	}
	c.composition.adoptLease = adopt
}

type rootSessionTransition struct {
	lease     *sessionstore.SessionLease
	authority *sessionstore.SessionWriteAuthority
	adopt     func(*sessionstore.SessionLease) error
}

func (c *Controller) prepareRootSessionTransition(path string) (*rootSessionTransition, error) {
	if c.rootEnvironment == nil {
		return nil, nil
	}
	if err := c.ensureWriteAuthorityReady(); err != nil {
		return nil, err
	}
	c.mu.Lock()
	var adopt func(*sessionstore.SessionLease) error
	if c.composition != nil {
		adopt = c.composition.adoptLease
	}
	c.mu.Unlock()
	if adopt == nil {
		return nil, sessionstore.ErrSessionWriteAuthorityMissing
	}
	lease, err := sessionstore.TryAcquireSessionLease(path)
	if err != nil {
		return nil, err
	}
	authority, err := lease.IssueWriteAuthority(sessionstore.NextSessionWriteGeneration())
	if err == nil {
		err = c.rootEnvironment.AttachSession(context.Background(), c.SessionPath(), path, authority)
	}
	if err != nil {
		lease.Release()
		return nil, err
	}
	return &rootSessionTransition{lease: lease, authority: authority, adopt: adopt}, nil
}

func (p *rootSessionTransition) close() {
	if p != nil && p.lease != nil {
		p.lease.Release()
	}
}

func (p *rootSessionTransition) commit(session *sessionstore.Session) error {
	if p == nil {
		return nil
	}
	if err := p.adopt(p.lease); err != nil {
		return err
	}
	session.BindWriteAuthority(p.authority)
	p.lease = nil
	return nil
}

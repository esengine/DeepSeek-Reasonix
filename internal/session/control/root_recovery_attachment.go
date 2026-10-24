package control

import (
	"context"

	"reasonix/internal/state/sessionstore"
)

func (c *Controller) rootRecoveryAttachment(parent string) func(string, *sessionstore.SessionPublication) error {
	if c.rootEnvironment == nil {
		return nil
	}
	return func(path string, publication *sessionstore.SessionPublication) error {
		lease, err := sessionstore.TryAcquireSessionLease(path)
		if err != nil {
			return err
		}
		defer lease.Release()
		authority, err := lease.IssueWriteAuthority(sessionstore.NextSessionWriteGeneration())
		if err != nil {
			return err
		}
		return c.rootEnvironment.AttachSessionPublication(context.Background(), parent, path, authority, publication)
	}
}

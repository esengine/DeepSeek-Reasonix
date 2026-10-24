package rootenv

import (
	"context"

	"reasonix/internal/state/sessionstore"
)

// Fork captures a separate writable scene while keeping the original source
// baseline. It never turns the parent's live worktree into a disposable result.
func (e *Environment) Fork(ctx context.Context, parent, path string, authority *sessionstore.SessionWriteAuthority) (*Environment, error) {
	retained, err := e.Retain()
	if err != nil {
		return nil, err
	}
	defer retained.Close()
	if err := retained.CheckSession(ctx, parent); err != nil {
		return nil, err
	}
	end, err := authority.BeginSave(path)
	if err != nil {
		return nil, err
	}
	defer end()
	r := retained.Record()
	var copies []string
	if r.Environment != nil {
		copies = r.Environment.Paths
	}
	next, err := retained.owner.store.prepareOwned(ctx, sessionstore.CanonicalSessionPath(path), copies, nil, &r)
	if err != nil {
		return nil, err
	}
	if err := next.BindSession(ctx, path, authority); err != nil {
		next.Close()
		return nil, err
	}
	return next, nil
}

package boot

import (
	"context"

	"reasonix/internal/contract/config"
	"reasonix/internal/session/control"
	"reasonix/internal/state/executionpresence"
	"reasonix/internal/state/sessionstore"
)

func (b *builder) buildExecutionPresence() (control.ExecutionPresence, error) {
	if b.session.mutations == nil {
		return nil, nil
	}
	root := b.root
	if b.repo.Valid() {
		root = b.repo.WorkTree
	}
	store, err := executionpresence.New(root, config.WorkspaceLeaseDir())
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context, path string, writer bool) (func(), error) {
		reservation, err := store.Reserve(ctx, executionpresence.Request{Strategy: executionpresence.Shared,
			Writer: writer, SessionID: sessionstore.BranchID(path)})
		if err != nil {
			return nil, err
		}
		if err := reservation.Activate(ctx, root); err != nil {
			reservation.Close()
			return nil, err
		}
		return func() { _ = reservation.Close() }, nil
	}, nil
}

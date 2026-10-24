package cli

import (
	"context"
	"errors"

	"reasonix/internal/assembly/boot"
	"reasonix/internal/frontend/clisession"
	"reasonix/internal/session/control"
)

// prepareRunRoot shares the host-owned first-execution transaction with serve.
// Its reservation bridges publication and synchronous turn admission.
func prepareRunRoot(ctx context.Context, old *control.Controller, leases *control.SessionLeaseKeeper, opts boot.Options) (*control.Controller, func(), error) {
	mode := old.RootEnvironmentStrategy()
	if old.RootEnvironment() != nil || (mode != "auto" && mode != "isolated") {
		return old, func() {}, nil
	}
	if mode == "auto" && !old.RootEnvironmentSelectionPending() {
		return old, func() {}, nil
	}
	release, err := old.BeginRuntimeReplacement()
	if err != nil {
		return old, nil, err
	}
	defer release()
	prepared, err := boot.PrepareRootExecution(ctx, old, opts)
	if err != nil {
		return old, nil, err
	}
	ok := false
	defer func() {
		if !ok {
			prepared.Close()
		}
	}()
	if prepared.Result != nil {
		if err := prepared.Adopt(ctx); err != nil {
			return old, nil, err
		}
		next := prepared.Controller
		if err := clisession.RebindAuthority(leases, next); err != nil {
			return old, nil, errors.Join(err, clisession.RebindAuthority(leases, old))
		}
		if err := prepared.Publish(func(*boot.BuildResult) error { return nil }); err != nil {
			return old, nil, errors.Join(err, clisession.RebindAuthority(leases, old))
		}
		old.ReleaseResources()
	}
	ok = true
	return prepared.Controller, prepared.Close, nil
}

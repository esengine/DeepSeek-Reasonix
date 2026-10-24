package cli

import (
	"context"

	"reasonix/internal/assembly/boot"
	"reasonix/internal/frontend/acp"
	"reasonix/internal/session/control"
)

func (f *acpFactory) PrepareRootExecution(ctx context.Context, p acp.SessionParams, old *control.Controller) (*boot.PreparedRootExecution, error) {
	opts, err := f.sessionBootOptions(p)
	if err != nil {
		return nil, err
	}
	return boot.PrepareRootExecution(ctx, old, opts)
}

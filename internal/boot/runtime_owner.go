package boot

import (
	"context"

	"reasonix/internal/extension"
)

func bindRuntimeOwner(ctx context.Context, opts Options) (context.Context, Options, *extension.RuntimeOwner, func(string, bool, []byte)) {
	owner := opts.Owner
	if owner == nil {
		owner = extension.NewRuntimeOwner()
		opts.Owner = owner
	}
	ctx = extension.ContextWithRuntimeOwner(ctx, owner)
	recordFileWrite := func(path string, hadPrior bool, prior []byte) {
		owner.RecordFileWrite(path, hadPrior, prior)
	}
	return ctx, opts, owner, recordFileWrite
}

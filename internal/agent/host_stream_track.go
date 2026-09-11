package agent

import (
	"context"

	"reasonix/internal/extension"
)

// trackPublishedHostStream binds cancel to the context's runtime owner so a
// session rebuild can drain its own HTTP streams without touching siblings.
func trackPublishedHostStream(ctx context.Context, cancel context.CancelFunc) (untrack func()) {
	owner := extension.RuntimeOwnerFromContext(ctx)
	gen := owner.Gate.Published()
	if gen == 0 || cancel == nil {
		return func() {}
	}
	return owner.HostStreams.Track(gen, cancel)
}

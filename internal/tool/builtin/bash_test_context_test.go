package builtin

import (
	"context"

	"reasonix/internal/sandbox"
)

func fullAccessBashTestContext(ctx context.Context) context.Context {
	return sandbox.WithPermissionPreset(ctx, "danger-full-access")
}

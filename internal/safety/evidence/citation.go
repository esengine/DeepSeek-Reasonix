package evidence

import (
	"context"
	"strings"
)

type workspaceRootKey struct{}

// WithWorkspaceRoot attaches the workspace a call's relative paths resolve
// against, so a cited path can be matched to the receipt for the same file.
func WithWorkspaceRoot(ctx context.Context, root string) context.Context {
	return context.WithValue(ctx, workspaceRootKey{}, strings.TrimSpace(root))
}

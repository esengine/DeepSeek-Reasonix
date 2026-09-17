package builtin

import (
	"context"
	"runtime"
	"testing"

	"reasonix/internal/sandbox"
)

func requirePOSIXShellTest(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell behavior is covered by Unix CI; Windows uses dedicated PowerShell and sandbox-policy tests")
	}
}

func fullAccessBashTestContext(ctx context.Context) context.Context {
	return sandbox.WithPermissionPreset(ctx, "danger-full-access")
}

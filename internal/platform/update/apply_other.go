//go:build !windows && !linux && !darwin

package update

import (
	"context"
	"fmt"
	"runtime"
)

func (v VersionedInstaller) apply(_ context.Context, _ Cached) error {
	return fmt.Errorf("update: self-update is unsupported on %s", runtime.GOOS)
}

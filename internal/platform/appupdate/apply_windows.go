//go:build windows

package appupdate

import (
	"context"

	"reasonix/internal/platform/update"
)

// applyDownloaded runs the next installer. Windows cannot replace a running
// executable, so nothing is staged here: the installer is what survives this
// process ending, and it is the whole install. The split is a build tag rather
// than a GOOS check so neither branch has to compile on the other platform.
func (c *capability) applyDownloaded(_ context.Context, _ update.Install, _, _ string, cached update.Cached) error {
	return c.opts.Line.RunInstaller(cached.Path)
}

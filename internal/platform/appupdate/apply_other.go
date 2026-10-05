//go:build !windows

package appupdate

import (
	"context"

	"reasonix/internal/platform/update"
)

// applyDownloaded stages the release into the versioned layout and swaps. The
// application it replaces is the one this shell stated: on macOS the unit is a
// bundle, and which bundle that is cannot be read back out of this process.
func (c *capability) applyDownloaded(ctx context.Context, install update.Install, _, dir string, cached update.Cached) error {
	inst := update.VersionedInstaller{
		Layout:  install.Layout,
		Staging: dir,
		Current: c.opts.Running,
		Line:    c.opts.Line,
		App:     c.opts.Application,
	}
	return inst.Install(ctx, cached)
}

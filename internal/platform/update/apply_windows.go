//go:build windows

package update

import (
	"context"
	"fmt"
)

// apply hands the verified installer to the update helper. Windows cannot
// replace a running executable, so the install happens after this process
// exits — the helper is what survives to observe whether it worked.
func (v VersionedInstaller) apply(_ context.Context, c Cached) error {
	if !v.Layout.Versioned() {
		return fmt.Errorf("update: %s predates the versioned layout; reinstall from the download page", v.Layout.Root)
	}
	return WindowsHandoff{
		InstallerPath:   c.Path,
		InstallerSHA256: c.SHA256,
		InstallDir:      v.Layout.Root,
		RelaunchPath:    v.Layout.Launcher,
		StagingDir:      v.Staging,
	}.StartVersioned(c.Version)
}

//go:build !windows

package installsource

import "testing"

func TestWindowsPluginUpdateWithExternalDirectoryWatch(t *testing.T) {
	t.Skip("requires Windows ReadDirectoryChangesW rename semantics")
}

func TestWindowsPluginWatchedUpdateRollbackAtPublication(t *testing.T) {
	t.Skip("requires Windows ReadDirectoryChangesW rename semantics")
}

func TestWindowsPluginWatchedUpdateRollbackOnStateWriteError(t *testing.T) {
	t.Skip("requires Windows directory watches and file sharing modes")
}

func TestWindowsPluginGenerationRequiresExplicitReplace(t *testing.T) {
	t.Skip("requires Windows side-by-side package replacement")
}

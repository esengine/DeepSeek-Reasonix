package boot

import (
	"testing"

	"reasonix/internal/contract/config"
)

// approveWorkspace approves what dir's configuration names, standing in for a
// person who ran `reasonix trust` there. Tests of the gate itself never call it.
func approveWorkspace(t *testing.T, dir string) {
	t.Helper()
	approveWorkspaceAt(t, "", dir)
}

// approveWorkspaceAt records the approval in a stated home; "" is the process's.
func approveWorkspaceAt(t *testing.T, home, dir string) {
	t.Helper()
	_, _ = config.RootsForHome(home).ApproveWorkspacePrograms(dir)
}

package boot

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/contract/config"
)

// approveWorkspace approves what dir's configuration names, standing in for a
// person who ran `reasonix trust` there. Tests of the gate itself never call it.
func approveWorkspace(t *testing.T, dir string) {
	t.Helper()
	_, _ = config.Roots{}.ApproveWorkspacePrograms(dir)
}

// mirrorToUserConfig copies dir's reasonix.toml into the user config, for a
// test whose model must reach folders other than dir.
func mirrorToUserConfig(t *testing.T, dir string) {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(dir, "reasonix.toml"))
	if err != nil {
		t.Fatal(err)
	}
	writeUserConfig(t, string(body))
}

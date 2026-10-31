package config

import (
	"testing"

	"reasonix/internal/base/testenv"
)

// The team's folder holds the secrets the user gave agents. It is denied to
// every reader in every Reasonix process, not only the one that registered the
// file: a command run by any session must not reach it.
func TestTeamDirIsDeniedToReaders(t *testing.T) {
	home := testenv.TempDir(t)
	cfg := Default()
	cfg.roots = RootsForHome(home)
	dir := cfg.Roots().TeamDir()
	if dir == "" {
		t.Fatal("no team dir")
	}
	for _, root := range cfg.ForbiddenRootsForRoot(testenv.TempDir(t)) {
		if samePath(root, dir) {
			return
		}
	}
	t.Fatalf("ForbiddenRootsForRoot does not list %s", dir)
}

package memory

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
)

// Copied from the audit worktree (audit/memory-index-recomputable): MEMORY.md is
// derived data the boot pass must be able to republish from the fact files.

func auditIndexPath(dir string) string { return filepath.Join(dir, indexFile) }

func auditReadIndex(t *testing.T, dir string) (string, bool) {
	t.Helper()
	raw, err := os.ReadFile(auditIndexPath(dir))
	if err != nil {
		return "unreadable: " + err.Error(), false
	}
	return string(raw), true
}

func auditSeed(t *testing.T, store Store, names ...string) {
	t.Helper()
	for _, name := range names {
		if _, err := store.SaveWithOptions(Memory{Name: name, Description: "d-" + name, Body: "v1"}, SaveOptions{}); err != nil {
			t.Fatal(err)
		}
	}
}

// A process that cannot read MEMORY.md (restored store with a foreign owner, an
// ACL change, a sync agent's partial file) must still be able to republish it:
// the index is derived from the facts, and every write path reads the old index
// first, so a read failure freezes the whole store. Today the one rebuild pass
// refuses to run because it cannot read the file it exists to repair.
func TestAuditIndexRebuildRunsWhenTheIndexIsUnreadable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod does not remove read permission on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("permission faults do not apply to root")
	}
	dir := testenv.TempDir(t)
	store := Store{Dir: dir}
	auditSeed(t, store, "alpha")

	index := auditIndexPath(dir)
	if err := os.Chmod(index, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(index, 0o644) })
	if _, err := os.ReadFile(index); err == nil {
		t.Fatal("fixture: MEMORY.md should be unreadable to this process")
	}

	// The premise: republishing does not need the old bytes. Proved on a
	// throwaway peer file so the fault under test stays in place.
	peer := filepath.Join(dir, "peer.md")
	if err := os.WriteFile(peer, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(peer, 0o000); err != nil {
		t.Fatal(err)
	}
	if err := writeMemoryAtomic(peer, []byte("y"), 0o644); err != nil {
		t.Fatalf("fixture: an atomic publish cannot replace a mode-000 file in a writable dir: %v", err)
	}
	if err := os.Remove(peer); err != nil {
		t.Fatal(err)
	}

	_, passErr := store.MigrateV2()
	got, readable := auditReadIndex(t, dir)
	if !readable || !strings.Contains(got, "(alpha.md)") {
		t.Fatalf("the boot maintenance pass left the derived index unrepaired: pass error = %v, MEMORY.md = %q; every memory write is refused while this file cannot be read, and no other entry (no re-index command, no doctor check, no HTTP endpoint) reaches this pass", passErr, got)
	}
}

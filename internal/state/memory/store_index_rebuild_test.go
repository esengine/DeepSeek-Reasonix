package memory

import (
	"os"
	"runtime"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
)

// A readable index is still repaired line by line: a stale line is refreshed and
// a missing one is appended, without a full rebuild.
func TestMigrateV2RepairsAStaleReadableIndex(t *testing.T) {
	dir := testenv.TempDir(t)
	store := Store{Dir: dir}
	auditSeed(t, store, "alpha", "beta")

	stale := "# Memory\n\n- [alpha](alpha.md) — [project/project] stale description\n"
	if err := os.WriteFile(auditIndexPath(dir), []byte(stale), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := store.MigrateV2(); err != nil {
		t.Fatal(err)
	}

	got := mustReadString(t, auditIndexPath(dir))
	if !strings.Contains(got, "d-alpha") || strings.Contains(got, "stale description") {
		t.Fatalf("the stale line was not refreshed:\n%s", got)
	}
	if !strings.Contains(got, "(beta.md)") {
		t.Fatalf("the missing line was not appended:\n%s", got)
	}
}

// A hand-written section is not managed state: a pass that rewrites managed
// lines must leave it byte for byte.
func TestMigrateV2KeepsHandWrittenTextWhenTheIndexIsReadable(t *testing.T) {
	dir := testenv.TempDir(t)
	store := Store{Dir: dir}
	auditSeed(t, store, "alpha")

	hand := "# Memory\n\nMy own note, not a fact.\n\n- [alpha](alpha.md) — [project/project] stale\n"
	if err := os.WriteFile(auditIndexPath(dir), []byte(hand), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := store.MigrateV2(); err != nil {
		t.Fatal(err)
	}

	got := mustReadString(t, auditIndexPath(dir))
	if !strings.Contains(got, "My own note, not a fact.") {
		t.Fatalf("the pass dropped hand-written text:\n%s", got)
	}
	if !strings.Contains(got, "d-alpha") {
		t.Fatalf("the pass left the managed line stale:\n%s", got)
	}
}

// Booting a healthy store must not churn the derived file: when every managed
// line is already right, the bytes on disk stay identical.
func TestMigrateV2LeavesAHealthyIndexByteIdentical(t *testing.T) {
	dir := testenv.TempDir(t)
	store := Store{Dir: dir}
	auditSeed(t, store, "alpha", "beta")

	before := mustReadString(t, auditIndexPath(dir))
	if _, err := store.MigrateV2(); err != nil {
		t.Fatal(err)
	}
	if after := mustReadString(t, auditIndexPath(dir)); after != before {
		t.Fatalf("a pass with nothing to repair rewrote the index:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// A missing index is derived data too: the pass recreates it from the facts.
func TestMigrateV2RebuildsAMissingIndex(t *testing.T) {
	dir := testenv.TempDir(t)
	store := Store{Dir: dir}
	auditSeed(t, store, "alpha", "beta")
	index := auditIndexPath(dir)
	if err := os.Remove(index); err != nil {
		t.Fatal(err)
	}

	if _, err := store.MigrateV2(); err != nil {
		t.Fatal(err)
	}
	got, readable := auditReadIndex(t, dir)
	if !readable {
		t.Fatal(got)
	}
	for _, want := range []string{"(alpha.md)", "(beta.md)"} {
		if !strings.Contains(got, want) {
			t.Fatalf("the recreated index is missing %s:\n%s", want, got)
		}
	}
}

// The freeze is the user-visible defect: after the pass republished the
// unreadable index, an ordinary save of another fact must succeed, and the
// rebuilt index must name every fact that could be read.
func TestIndexRebuildUnblocksLaterSaves(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod does not remove read permission on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("permission faults do not apply to root")
	}
	dir := testenv.TempDir(t)
	store := Store{Dir: dir}
	auditSeed(t, store, "alpha", "gamma")

	index := auditIndexPath(dir)
	if err := os.Chmod(index, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(index, 0o644) })
	if _, err := os.ReadFile(index); err == nil {
		t.Fatal("fixture: MEMORY.md should be unreadable to this process")
	}

	if _, err := store.MigrateV2(); err != nil {
		t.Fatalf("the boot pass reported the read fault instead of republishing: %v", err)
	}
	got, readable := auditReadIndex(t, dir)
	if !readable {
		t.Fatal(got)
	}
	for _, want := range []string{"(alpha.md)", "(gamma.md)"} {
		if !strings.Contains(got, want) {
			t.Fatalf("the rebuilt index does not name %s:\n%s", want, got)
		}
	}

	if _, err := store.Save(Memory{Name: "beta", Description: "d-beta", Body: "v1"}); err != nil {
		t.Fatalf("a normal save still fails after the rebuild: %v", err)
	}
	got = mustReadString(t, auditIndexPath(dir))
	for _, want := range []string{"(alpha.md)", "(gamma.md)", "(beta.md)"} {
		if !strings.Contains(got, want) {
			t.Fatalf("the index after the save is missing %s:\n%s", want, got)
		}
	}
	if _, found, err := store.Read("beta"); !found || err != nil {
		t.Fatalf("the saved fact is not readable: found = %v, err = %v", found, err)
	}
}

package memory

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
)

// indexAsides lists the stamped copies an unreadable index was moved to.
func indexAsides(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), indexFile+".unreadable-") {
			out = append(out, filepath.Join(dir, entry.Name()))
		}
	}
	return out
}

// makeIndexUnreadable replaces MEMORY.md with the given bytes and removes read
// permission, so this process cannot read it but can still rename it away.
func makeIndexUnreadable(t *testing.T, dir, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("chmod does not remove read permission on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("permission faults do not apply to root")
	}
	index := auditIndexPath(dir)
	if err := os.WriteFile(index, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(index, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(index, 0o644) })
	if _, err := os.ReadFile(index); err == nil {
		t.Fatal("fixture: MEMORY.md should be unreadable to this process")
	}
	return index
}

// A read failure that is not a property of the file (an I/O error, a resource
// limit) may leave a perfectly good index with hand-written text. The pass must
// return that error and touch nothing, not republish derived lines over bytes
// that could still be read tomorrow.
func TestMigrateV2DoesNotRebuildOnATransientIndexReadError(t *testing.T) {
	dir := testenv.TempDir(t)
	store := Store{Dir: dir}
	auditSeed(t, store, "alpha")
	index := auditIndexPath(dir)
	hand := "# Memory\n\nMy own note, not a fact.\n"
	if err := os.WriteFile(index, []byte(hand), 0o644); err != nil {
		t.Fatal(err)
	}
	withFault(t, func(op, path string) error {
		if op == "read-index" && path == index {
			return &os.PathError{Op: "read", Path: path, Err: io.ErrUnexpectedEOF}
		}
		return nil
	})

	if _, err := store.MigrateV2(); err == nil {
		t.Fatal("the pass reported success although the index could not be read")
	}
	if got := mustReadString(t, index); got != hand {
		t.Fatalf("a transient read failure republished the index over bytes that might still be good:\nbefore:\n%s\nafter:\n%s", hand, got)
	}
	if asides := indexAsides(t, dir); len(asides) != 0 {
		t.Fatalf("a transient read failure moved the index aside: %v", asides)
	}
}

// The bytes of an unreadable index are not merged into the rebuild (they cannot
// be read), but they must survive: the rebuild renames the entry to a stamped
// name first, and a later repair can still read it.
func TestMigrateV2KeepsTheUnreadableIndexAside(t *testing.T) {
	dir := testenv.TempDir(t)
	store := Store{Dir: dir}
	auditSeed(t, store, "alpha")
	hand := "# Memory\n\nMy own note, not a fact.\n"
	makeIndexUnreadable(t, dir, hand)

	if _, err := store.MigrateV2(); err != nil {
		t.Fatalf("the pass did not repair a permission-denied index: %v", err)
	}
	got, readable := auditReadIndex(t, dir)
	if !readable || !strings.Contains(got, "(alpha.md)") {
		t.Fatalf("the rebuilt index does not name the readable fact: readable = %v, MEMORY.md = %q", readable, got)
	}

	asides := indexAsides(t, dir)
	if len(asides) != 1 {
		t.Fatalf("want exactly one stamped aside copy of the unreadable index, got %v", asides)
	}
	if err := os.Chmod(asides[0], 0o600); err != nil {
		t.Fatal(err)
	}
	if body := mustReadString(t, asides[0]); body != hand {
		t.Fatalf("the aside copy does not hold the unreadable bytes:\n%s", body)
	}
}

// A directory named MEMORY.md exists but is not a regular file: it is a
// deterministic fault, and a rename moves the whole directory aside without
// reading it, so the pass repairs the store instead of freezing it.
func TestMigrateV2RebuildsWhenTheIndexIsADirectory(t *testing.T) {
	dir := testenv.TempDir(t)
	store := Store{Dir: dir}
	auditSeed(t, store, "alpha")
	index := auditIndexPath(dir)
	if err := os.Remove(index); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(index, 0o755); err != nil {
		t.Fatal(err)
	}
	leftover := filepath.Join(index, "hand-written.txt")
	if err := os.WriteFile(leftover, []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := store.MigrateV2(); err != nil {
		t.Fatalf("a directory named MEMORY.md is a property of the entry and must be repaired, not reported: %v", err)
	}
	got, readable := auditReadIndex(t, dir)
	if !readable || !strings.Contains(got, "(alpha.md)") {
		t.Fatalf("the rebuilt index does not name the readable fact: readable = %v, MEMORY.md = %q", readable, got)
	}
	asides := indexAsides(t, dir)
	if len(asides) != 1 {
		t.Fatalf("want exactly one stamped aside copy of the directory, got %v", asides)
	}
	if body := mustReadString(t, filepath.Join(asides[0], "hand-written.txt")); body != "keep me" {
		t.Fatalf("the directory's contents did not survive the move: %q", body)
	}
}

// Moving the entry aside is a precondition for publishing: if the rename fails,
// the pass must report the original read fault and publish nothing, so the old
// bytes are never replaced when they could not be kept.
func TestMigrateV2ReturnsTheReadFaultWhenTheAsideRenameFails(t *testing.T) {
	dir := testenv.TempDir(t)
	store := Store{Dir: dir}
	auditSeed(t, store, "alpha")
	hand := "# Memory\n\nMy own note, not a fact.\n"
	index := makeIndexUnreadable(t, dir, hand)
	blocked := errors.New("aside rename blocked")
	withFault(t, func(op, path string) error {
		if op == "rename" && path == index {
			return blocked
		}
		return nil
	})

	_, err := store.MigrateV2()
	if err == nil {
		t.Fatal("the pass reported success although the unreadable index could not be moved aside")
	}
	if !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("error = %v, want the original read fault", err)
	}
	if errors.Is(err, blocked) {
		t.Fatalf("returned the aside rename failure instead of the original read fault: %v", err)
	}
	if asides := indexAsides(t, dir); len(asides) != 0 {
		t.Fatalf("an aside copy appeared although the rename reported failure: %v", asides)
	}
	if err := os.Chmod(index, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := mustReadString(t, index); got != hand {
		t.Fatalf("the index was republished although it could not be moved aside:\nbefore:\n%s\nafter:\n%s", hand, got)
	}
}

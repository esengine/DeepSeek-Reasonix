package observation

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"reasonix/internal/state/trustedstate"
)

func tree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		write(t, root, rel, body)
	}
	return root
}

func write(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func observer(t *testing.T) (*Observer, string) {
	t.Helper()
	dir := t.TempDir()
	return NewObserver(trustedstate.Open(dir, nil), DefaultPolicy()), dir
}

func objectCount(t *testing.T, dir string) int {
	t.Helper()
	n := 0
	_ = filepath.WalkDir(filepath.Join(dir, "objects"), func(_ string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			n++
		}
		return nil
	})
	return n
}

func TestUnchangedTreeRecordsNothingNew(t *testing.T) {
	root := tree(t, map[string]string{"a.go": "a", "pkg/b.go": "b", "pkg/deep/c.go": "c"})
	o, dir := observer(t)
	first := o.Take(context.Background(), root)
	if !first.Complete || first.Entries != 5 {
		t.Fatalf("first = %+v, want complete with 5 entries", first)
	}
	stored := objectCount(t, dir)

	again := NewObserver(trustedstate.Open(dir, nil), DefaultPolicy()).Take(context.Background(), root)
	if again.Digest != first.Digest || objectCount(t, dir) != stored {
		t.Fatalf("an unchanged tree produced %s (was %s) and %d objects (was %d)", again.Digest, first.Digest, objectCount(t, dir), stored)
	}
}

func TestChangedListsEveryDifference(t *testing.T) {
	root := tree(t, map[string]string{"keep.go": "k", "edit.go": "old", "gone/x.go": "x", "pkg/y.go": "y"})
	o, _ := observer(t)
	before := o.Take(context.Background(), root)

	time.Sleep(10 * time.Millisecond)
	write(t, root, "edit.go", "new body")
	write(t, root, "pkg/added.go", "z")
	if err := os.RemoveAll(filepath.Join(root, "gone")); err != nil {
		t.Fatal(err)
	}
	after := o.Take(context.Background(), root)

	paths, count, err := o.Changed(before, after, 100)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"edit.go", "gone", "gone/x.go", "pkg/added.go"}
	slices.Sort(paths)
	if !slices.Equal(paths, want) || count != len(want) {
		t.Fatalf("Changed = %v (%d), want %v", paths, count, want)
	}
	if _, count, _ := o.Changed(before, before, 100); count != 0 {
		t.Fatalf("a snapshot differs from itself by %d", count)
	}
}

func TestChangedStopsListingAtLimitButKeepsCounting(t *testing.T) {
	root := tree(t, nil)
	o, _ := observer(t)
	before := o.Take(context.Background(), root)
	for _, f := range []string{"a", "b", "c"} {
		write(t, root, f, f)
	}
	paths, count, err := o.Changed(before, o.Take(context.Background(), root), 2)
	if err != nil || len(paths) != 2 || count != 3 {
		t.Fatalf("Changed = %v, %d, %v; want 2 listed of 3", paths, count, err)
	}
}

func TestVCSStoreIsOutsideTheObservationDomain(t *testing.T) {
	root := tree(t, map[string]string{"main.go": "m", ".git/HEAD": "ref"})
	o, _ := observer(t)
	before := o.Take(context.Background(), root)
	write(t, root, ".git/index", "rewritten by git status")
	if after := o.Take(context.Background(), root); after.Digest != before.Digest {
		t.Fatal("a write inside the VCS store changed the snapshot")
	}
}

func TestSymlinkIsRecordedNotFollowed(t *testing.T) {
	outside := tree(t, map[string]string{"secret": "s"})
	root := tree(t, map[string]string{"a.go": "a"})
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	o, _ := observer(t)
	before := o.Take(context.Background(), root)
	write(t, outside, "secret", "changed behind the link")
	if after := o.Take(context.Background(), root); after.Digest != before.Digest || after.Entries != 2 {
		t.Fatalf("the snapshot followed the link: %+v", after)
	}
}

func TestEntryLimitMakesTheSnapshotIncomplete(t *testing.T) {
	root := tree(t, map[string]string{"a": "a", "b": "b", "c": "c"})
	store := trustedstate.Open(t.TempDir(), nil)
	p := DefaultPolicy()
	p.MaxEntries = 2
	o := NewObserver(store, p)
	s := o.Take(context.Background(), root)
	if s.Complete || s.Incomplete != IncompleteEntryLimit {
		t.Fatalf("snapshot = %+v, want incomplete at the entry limit", s)
	}
	if _, _, err := o.Changed(s, s, 10); !errors.Is(err, ErrIncomparable) {
		t.Fatalf("an incomplete snapshot compared: %v", err)
	}
}

func TestCancelledSnapshotIsIncomplete(t *testing.T) {
	root := tree(t, map[string]string{"a": "a"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	o, _ := observer(t)
	if s := o.Take(ctx, root); s.Complete || s.Incomplete != IncompleteCancelled {
		t.Fatalf("snapshot = %+v, want cancelled", s)
	}
}

func TestSnapshotsUnderDifferentPoliciesDoNotCompare(t *testing.T) {
	root := tree(t, map[string]string{"a": "a"})
	store := trustedstate.Open(t.TempDir(), nil)
	narrow := DefaultPolicy()
	narrow.Excluded = append(narrow.Excluded, Exclusion{Name: "vendor", Reason: "test"})
	a := NewObserver(store, DefaultPolicy()).Take(context.Background(), root)
	b := NewObserver(store, narrow).Take(context.Background(), root)
	if _, _, err := NewObserver(store, DefaultPolicy()).Changed(a, b, 10); !errors.Is(err, ErrIncomparable) {
		t.Fatalf("snapshots under two policies compared: %v", err)
	}
}

func TestUnwritableStoreMakesTheSnapshotIncomplete(t *testing.T) {
	root := tree(t, map[string]string{"a": "a"})
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if s := NewObserver(trustedstate.Open(file, nil), DefaultPolicy()).Take(context.Background(), root); s.Complete || s.Incomplete != IncompleteStore {
		t.Fatalf("snapshot = %+v, want incomplete for the store", s)
	}
}

func TestHostStateRootIsOutsideTheObservationDomain(t *testing.T) {
	root := tree(t, map[string]string{"main.go": "m", "home/.reasonix/trusted/HEAD": "1"})
	store := trustedstate.Open(t.TempDir(), nil)
	o := NewObserver(store, DefaultPolicy(filepath.Join(root, "home", ".reasonix")))
	before := o.Take(context.Background(), root)
	write(t, root, "home/.reasonix/trusted/HEAD", "2")
	if after := o.Take(context.Background(), root); after.Digest != before.Digest {
		t.Fatal("the host's own writes changed the snapshot")
	}
}

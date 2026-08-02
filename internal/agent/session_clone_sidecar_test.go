package agent

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/provider"
	"reasonix/internal/store"
)

// TestCloneRefusesPreExistingSidecars verifies the ownership contract: a
// pre-existing authoritative event log, event index or branch metadata at the
// destination is refused and its bytes stay untouched — the clone never
// deletes data it did not create.
func TestCloneRefusesPreExistingSidecars(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	dir := t.TempDir()
	src := filepath.Join(dir, "source.jsonl")
	if err := (&Session{Messages: []provider.Message{{Role: "user", Content: "hi"}}}).Save(src); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		path func(string) string
	}{
		{name: "checkpoint", path: func(path string) string { return path }},
		{name: "event log", path: store.SessionEventLog},
		{name: "event index", path: store.SessionEventIndex},
		{name: "branch metadata", path: store.SessionMeta},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dst := filepath.Join(dir, tc.name+"-copy.jsonl")
			preExisting := tc.path(dst)
			want := "pre-existing " + tc.name + "\n"
			if err := os.WriteFile(preExisting, []byte(want), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := CloneSessionToPath(src, dst); err == nil {
				t.Fatal("clone over a pre-existing destination artifact must fail")
			}
			b, err := os.ReadFile(preExisting)
			if err != nil {
				t.Fatalf("pre-existing file removed: %s: %v", preExisting, err)
			}
			if string(b) != want {
				t.Fatalf("pre-existing file modified: %q, want %q", b, want)
			}
			for _, path := range []string{dst, store.SessionEventLog(dst), store.SessionEventIndex(dst), store.SessionMeta(dst)} {
				if path == preExisting {
					continue
				}
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatalf("partial clone artifact survived at %s: %v", path, err)
				}
			}
		})
	}
}

func TestSessionCloneDiscardUsesOwnedPaths(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	dir := t.TempDir()
	src := filepath.Join(dir, "source.jsonl")
	if err := (&Session{Messages: []provider.Message{{Role: "user", Content: "hi"}}}).Save(src); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "copy.jsonl")
	clone, err := CloneSessionToPath(src, dst)
	if err != nil {
		t.Fatal(err)
	}
	if err := clone.Discard(); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{dst, store.SessionEventLog(dst), store.SessionEventIndex(dst), store.SessionMeta(dst)} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("discard left clone-owned artifact %s: %v", path, err)
		}
	}
}

func TestSessionCloneDiscardRefusesChangedArtifact(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	dir := t.TempDir()
	src := filepath.Join(dir, "source.jsonl")
	if err := (&Session{Messages: []provider.Message{{Role: "user", Content: "hi"}}}).Save(src); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "copy.jsonl")
	clone, err := CloneSessionToPath(src, dst)
	if err != nil {
		t.Fatal(err)
	}
	changedPath := store.SessionMeta(dst)
	want := []byte("adopted by another runtime\n")
	if err := os.WriteFile(changedPath, want, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := clone.Discard(); !errors.Is(err, ErrSessionCloneChanged) {
		t.Fatalf("discard changed clone error = %v, want ErrSessionCloneChanged", err)
	}

	got, err := os.ReadFile(changedPath)
	if err != nil {
		t.Fatalf("discard deleted an artifact changed after clone: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("changed artifact = %q, want %q", got, want)
	}
	for _, path := range []string{dst, store.SessionEventLog(dst), store.SessionEventIndex(dst)} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("fail-closed discard removed unchanged sibling %s: %v", path, err)
		}
	}
}

func TestSessionCloneCommitTransfersDestinationLease(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	dir := t.TempDir()
	src := filepath.Join(dir, "source.jsonl")
	if err := (&Session{Messages: []provider.Message{{Role: "user", Content: "hi"}}}).Save(src); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "copy.jsonl")
	clone, err := CloneSessionToPath(src, dst)
	if err != nil {
		t.Fatal(err)
	}
	if competing, err := TryAcquireSessionLease(dst); !errors.Is(err, ErrSessionLeaseHeld) {
		if competing != nil {
			competing.Release()
		}
		t.Fatalf("uncommitted clone lease was not exclusive: %v", err)
	}
	lease := clone.Commit()
	if lease == nil {
		t.Fatal("commit did not transfer the destination lease")
	}
	if competing, err := TryAcquireSessionLease(dst); !errors.Is(err, ErrSessionLeaseHeld) {
		if competing != nil {
			competing.Release()
		}
		lease.Release()
		t.Fatalf("transferred clone lease was not exclusive: %v", err)
	}
	lease.Release()
	acquired, err := TryAcquireSessionLease(dst)
	if err != nil {
		t.Fatalf("destination lease unavailable after release: %v", err)
	}
	acquired.Release()
}

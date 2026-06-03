package checkpoint

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/diff"
)

func write(t *testing.T, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}
func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// Two turns edit a.txt and create b.txt; rewinding restores each file to its
// state at the start of the chosen turn (b.txt being deleted when it post-dates it).
func TestRestoreToStartOfTurn(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "a.txt")
	b := filepath.Join(root, "sub", "b.txt")
	write(t, a, "v0")
	s := New("", root)

	s.Begin(0, "first", 0)
	s.Snapshot(diff.Change{Path: a, Kind: diff.Modify, OldText: "v0"})
	write(t, a, "v1") // the edit turn 0 made

	s.Begin(1, "second", 2)
	s.Snapshot(diff.Change{Path: a, Kind: diff.Modify, OldText: "v1"})
	s.Snapshot(diff.Change{Path: b, Kind: diff.Create})
	write(t, a, "v2")
	write(t, b, "new")

	// Rewind to the start of turn 1: a back to v1, b gone.
	if _, _, err := s.RestoreCode(1); err != nil {
		t.Fatal(err)
	}
	if got := read(t, a); got != "v1" {
		t.Fatalf("a = %q, want v1", got)
	}
	if _, err := os.Stat(b); !os.IsNotExist(err) {
		t.Fatalf("b should have been deleted, stat err=%v", err)
	}
}

func TestRestoreToTurnZero(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "a.txt")
	write(t, a, "v0")
	s := New("", root)
	s.Begin(0, "first", 0)
	s.Snapshot(diff.Change{Path: a, Kind: diff.Modify, OldText: "v0"})
	write(t, a, "v1")
	s.Begin(1, "second", 2)
	s.Snapshot(diff.Change{Path: a, Kind: diff.Modify, OldText: "v1"})
	write(t, a, "v2")

	if _, _, err := s.RestoreCode(0); err != nil {
		t.Fatal(err)
	}
	if got := read(t, a); got != "v0" {
		t.Fatalf("a = %q, want v0 (earliest snapshot)", got)
	}
}

func TestSnapshotDedupsFirstTouchWins(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "a.txt")
	write(t, a, "orig")
	s := New("", root)
	s.Begin(0, "p", 0)
	s.Snapshot(diff.Change{Path: a, Kind: diff.Modify, OldText: "orig"})
	s.Snapshot(diff.Change{Path: a, Kind: diff.Modify, OldText: "edited-once"}) // ignored
	write(t, a, "edited-twice")
	if _, _, err := s.RestoreCode(0); err != nil {
		t.Fatal(err)
	}
	if got := read(t, a); got != "orig" {
		t.Fatalf("a = %q, want orig (first snapshot wins)", got)
	}
}

func TestRestoreRejectsPathEscape(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "evil.txt")
	write(t, outside, "keep")
	s := New("", root)
	s.Begin(0, "p", 0)
	s.Snapshot(diff.Change{Path: outside, Kind: diff.Modify, OldText: "hacked"})
	if _, _, err := s.RestoreCode(0); err == nil {
		t.Fatal("RestoreCode should reject a path outside the workspace")
	}
	if got := read(t, outside); got != "keep" {
		t.Fatalf("outside file was modified: %q", got)
	}
}

func TestPersistenceRoundTrip(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(t.TempDir(), "sess.ckpt")
	a := filepath.Join(root, "a.txt")

	s := New(dir, root)
	s.Begin(0, "hello", 1)
	s.Snapshot(diff.Change{Path: a, Kind: diff.Modify, OldText: "v0"})
	s.Begin(1, "world", 5)

	// A fresh store over the same dir must see both turns and their boundaries.
	s2 := New(dir, root)
	metas := s2.List()
	if len(metas) != 2 {
		t.Fatalf("loaded %d checkpoints, want 2", len(metas))
	}
	if metas[0].Prompt != "hello" || metas[1].Prompt != "world" {
		t.Fatalf("prompts = %q, %q", metas[0].Prompt, metas[1].Prompt)
	}
	// Boundaries must survive the round-trip so a resumed session can rewind/fork.
	b := s2.Bounds()
	if b[0] != 1 || b[1] != 5 {
		t.Fatalf("bounds = %v, want {0:1, 1:5}", b)
	}
	if s2.NextTurn() != 2 {
		t.Fatalf("NextTurn = %d, want 2", s2.NextTurn())
	}
}

func TestLazyDirectoryCreation(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(t.TempDir(), "lazy-sess.ckpt")

	// 1. Construct a Store with a non-empty dir string
	s := New(dir, root)

	// 2. Assert the directory does NOT exist yet
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("directory should not exist yet: %v", err)
	}

	// 3. Write/persist one checkpoint (via Begin)
	s.Begin(0, "lazy", 0)

	// 4. Assert the directory and the turn file now exist
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("directory should now exist: %v", err)
	}
	turnPath := filepath.Join(dir, "turn-0.json")
	if _, err := os.Stat(turnPath); err != nil {
		t.Fatalf("turn file should now exist: %v", err)
	}
}

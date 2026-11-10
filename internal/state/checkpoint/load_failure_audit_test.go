package checkpoint

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
)

// TestAuditUnreadableCheckpointIsSilentlyForgotten pins what the store must do
// when a persisted turn file is present but unreadable: the corrupt record has
// to be distinguishable from a turn that never existed, and it must not be
// silently overwritten by the next turn. The file below is the store's own,
// truncated the way an interrupted write, a bad sector, or an editor leaves it.
func TestAuditUnreadableCheckpointIsSilentlyForgotten(t *testing.T) {
	dir := testenv.TempDir(t)
	root := testenv.TempDir(t)

	// A healthy turn-1 checkpoint, produced by the store itself so the layout
	// and JSON shape are the production ones.
	seed := New(dir, root)
	seed.Begin(1, "first prompt", 7)
	if err := seed.persist(seed.cur); err != nil {
		t.Fatalf("seed persist: %v", err)
	}
	path := filepath.Join(dir, "turn-1.json")
	good, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("seed file: %v", err)
	}
	var parsed Checkpoint
	if json.Unmarshal(good, &parsed) != nil {
		t.Fatalf("seed file is not valid checkpoint JSON: %s", good)
	}

	// The store reads it back: the rewind boundary for turn 1 is known.
	healthy := New(dir, root)
	if got := healthy.Bounds(); got[1] != 7 {
		t.Fatalf("healthy fixture: Bounds()[1] = %d, want 7", got[1])
	}

	// Now the same file, truncated the way an interrupted write leaves it.
	corrupt := []byte(good[:len(good)/2])
	if err := os.WriteFile(path, corrupt, 0o644); err != nil {
		t.Fatalf("truncate: %v", err)
	}

	store := New(dir, root)
	if _, ok := store.Bounds()[1]; ok {
		t.Fatalf("truncated checkpoint was accepted as a boundary")
	}

	// The next turn must land above the lost one, not restart below it: the
	// number whose file on disk is the unreadable record belongs to that
	// record, and reusing it overwrites the only remaining copy.
	if err := store.Begin(store.NextTurn(), "second prompt", 9); err != nil {
		t.Fatalf("begin: %v", err)
	}
	// The contract: a turn on disk but unreadable must not read as one that
	// never happened. load() drops the file on any read or parse error with no
	// record that it failed, so this assertion cannot be satisfied today.
	if next := store.NextTurn(); next != 3 {
		t.Fatalf("unreadable turn-1 checkpoint was forgotten and its turn number reused: NextTurn() = %d, want 3; Bounds() = %v; stored turn in turn-1.json = %d, original on-disk bytes were %s",
			next, store.Bounds(), parsed.Turn, corrupt)
	}
	if _, err := os.Stat(filepath.Join(dir, "turn-0.json")); !os.IsNotExist(err) {
		t.Fatalf("the store reused a turn number below the lost one: turn-0.json stat err = %v", err)
	}
}

// TestAuditAbsentTurnStillNumbersNormally is the control for a turn that truly
// never happened: no file, so no reservation, so no invented gap.
func TestAuditAbsentTurnStillNumbersNormally(t *testing.T) {
	dir := testenv.TempDir(t)
	root := testenv.TempDir(t)

	seed := New(dir, root)
	seed.Begin(0, "first", 3)
	if err := seed.persist(seed.cur); err != nil {
		t.Fatalf("persist turn 0: %v", err)
	}
	seed.Begin(2, "third", 9)
	if err := seed.persist(seed.cur); err != nil {
		t.Fatalf("persist turn 2: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "turn-1.json")); !os.IsNotExist(err) {
		t.Fatalf("fixture must leave turn 1 absent, stat err = %v", err)
	}

	store := New(dir, root)
	if got := store.UnreadableTurns(); len(got) != 0 {
		t.Fatalf("an absent turn was reported unreadable: %v", got)
	}
	if got := store.NextTurn(); got != 3 {
		t.Fatalf("absent turn invented a gap: NextTurn() = %d, want 3; Bounds() = %v", got, store.Bounds())
	}
	if got := store.Bounds(); len(got) != 2 || got[0] != 3 || got[2] != 9 {
		t.Fatalf("Bounds() = %v, want map[0:3 2:9]", got)
	}
}

// TestAuditExpiredCheckpointStillLoads is the control for the expired-payload
// directory: its metadata is readable, so it must keep loading with the same
// boundaries and reserve nothing.
func TestAuditExpiredCheckpointStillLoads(t *testing.T) {
	dir := testenv.TempDir(t)
	root := testenv.TempDir(t)
	content := "before"

	seed := New(dir, root)
	seed.Begin(1, "first", 7)
	seed.cur.Files = []FileSnap{{Path: "a.txt", Content: &content, SHA256: Digest([]byte(content))}}
	if err := seed.persist(seed.cur); err != nil {
		t.Fatalf("persist turn 1: %v", err)
	}
	seed.mu.Lock()
	err := seed.expirePayloadLocked(seed.cur)
	seed.mu.Unlock()
	if err != nil {
		t.Fatalf("expire payload: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "expired", "turn-1.json")); err != nil {
		t.Fatalf("fixture did not move turn 1 under expired/: %v", err)
	}

	store := New(dir, root)
	if got := store.UnreadableTurns(); len(got) != 0 {
		t.Fatalf("a readable expired checkpoint was reported unreadable: %v", got)
	}
	if got := store.Bounds(); len(got) != 1 || got[1] != 7 {
		t.Fatalf("Bounds() = %v, want map[1:7]", got)
	}
	if got := store.NextTurn(); got != 2 {
		t.Fatalf("NextTurn() = %d, want 2", got)
	}
}

// TestAuditUnreadableTurnBytesSurviveLaterBegins is the control for the other
// half of the contract: numbering past the lost turn is only useful if the
// unreadable file itself is left byte-identical.
func TestAuditUnreadableTurnBytesSurviveLaterBegins(t *testing.T) {
	dir := testenv.TempDir(t)
	root := testenv.TempDir(t)

	seed := New(dir, root)
	seed.Begin(1, "first", 7)
	if err := seed.persist(seed.cur); err != nil {
		t.Fatalf("persist turn 1: %v", err)
	}
	path := filepath.Join(dir, "turn-1.json")
	good, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("seed file: %v", err)
	}
	corrupt := []byte(good[:len(good)/2])
	if err := os.WriteFile(path, corrupt, 0o644); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	before := append([]byte(nil), corrupt...)

	store := New(dir, root)
	if got := store.UnreadableTurns(); len(got) != 1 || got[0] != 1 {
		t.Fatalf("UnreadableTurns() = %v, want [1]", got)
	}
	if err := store.Begin(store.NextTurn(), "second", 9); err != nil {
		t.Fatalf("begin second: %v", err)
	}
	if err := store.Begin(store.NextTurn(), "third", 11); err != nil {
		t.Fatalf("begin third: %v", err)
	}
	for _, name := range []string{"turn-2.json", "turn-3.json"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("later turn was not written as %s: %v", name, err)
		}
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("after later turns: %v", err)
	}
	if !bytes.Equal(after, before) {
		t.Fatalf("turn-1.json was rewritten: got %q, want the original unreadable bytes %q", after, before)
	}

	reloaded := New(dir, root)
	if got := reloaded.UnreadableTurns(); len(got) != 1 || got[0] != 1 {
		t.Fatalf("reloaded UnreadableTurns() = %v, want [1]", got)
	}
	if got := reloaded.NextTurn(); got != 4 {
		t.Fatalf("reloaded NextTurn() = %d, want 4; Bounds() = %v", got, reloaded.Bounds())
	}
}

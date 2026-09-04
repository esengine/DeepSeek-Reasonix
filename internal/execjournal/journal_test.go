package execjournal

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/store"
)

func sessionPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "session.jsonl")
}

// TestOpeningSurvivesWithoutTheTurn is the whole point of the file: a turn is
// appended when it ends, so the journal has to answer for a delegation whose
// turn never reached the transcript.
func TestOpeningSurvivesWithoutTheTurn(t *testing.T) {
	path := sessionPath(t)
	if err := Open(path, Opening{ID: "call/item-1", Group: "call", Turn: "turn-a", Name: "research", Grant: "read"}); err != nil {
		t.Fatalf("open: %v", err)
	}
	entries := History(path)
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	got := entries[0]
	if got.ID != "call/item-1" || got.Group != "call" || got.Turn != "turn-a" || got.Grant != "read" {
		t.Fatalf("entry = %+v, want the opening's identity, group, turn and grant", got)
	}
	if !got.Open() {
		t.Fatal("entry is settled; nothing settled it")
	}
}

// TestOpenIsNotInterruptedWhileThisProcessOwnsIt separates the two states the
// journal on disk cannot tell apart. Without the live set every running item
// would read as interrupted in the process that is running it.
func TestOpenIsNotInterruptedWhileThisProcessOwnsIt(t *testing.T) {
	path := sessionPath(t)
	if err := Open(path, Opening{ID: "call/item-1"}); err != nil {
		t.Fatalf("open: %v", err)
	}
	if got := Interrupted(path); len(got) != 0 {
		t.Fatalf("interrupted = %+v, want none while this process owns it", got)
	}
	// The next process holds no claims on this session, and reads the same file.
	Disown(path)
	if got := Interrupted(path); len(got) != 1 {
		t.Fatalf("interrupted after the owner is gone = %d, want 1", len(got))
	}
}

func TestSettledExecutionIsNeverInterrupted(t *testing.T) {
	path := sessionPath(t)
	if err := Open(path, Opening{ID: "call/item-1"}); err != nil {
		t.Fatalf("open: %v", err)
	}
	Settle(path, "call/item-1")
	Disown(path)
	if got := Interrupted(path); len(got) != 0 {
		t.Fatalf("interrupted = %+v, want none after settling", got)
	}
}

// TestAdoptedOpeningIsClosedOnArrival holds the one disposition that never
// executes. An adopted item has no owner to go missing, so reporting it as
// interrupted would invent work nobody did.
func TestAdoptedOpeningIsClosedOnArrival(t *testing.T) {
	path := sessionPath(t)
	if err := Open(path, Opening{ID: "call/item-1", Disposition: DispositionAdopted}); err != nil {
		t.Fatalf("open: %v", err)
	}
	if entries := History(path); len(entries) != 1 || entries[0].Open() {
		t.Fatalf("entries = %+v, want one closed entry", entries)
	}
	Disown(path)
	if got := Interrupted(path); len(got) != 0 {
		t.Fatalf("interrupted = %+v, want none for an adopted item", got)
	}
}

// TestSettlingAloneInventsNoHistory: a torn or replayed tail must not conjure an
// execution the journal never recorded opening.
func TestSettlingAloneInventsNoHistory(t *testing.T) {
	path := sessionPath(t)
	Settle(path, "call/item-1")
	if entries := History(path); len(entries) != 0 {
		t.Fatalf("entries = %+v, want none from a settling with no opening", entries)
	}
}

// TestFirstSettlingWins: the fan-out settles each item as it lands and again
// when the group ends, so a duplicate is the normal case, not a corrupt one.
func TestFirstSettlingWins(t *testing.T) {
	path := sessionPath(t)
	if err := Open(path, Opening{ID: "call/item-1"}); err != nil {
		t.Fatalf("open: %v", err)
	}
	Settle(path, "call/item-1")
	first := History(path)[0].SettledAt
	Settle(path, "call/item-1")
	entries := History(path)
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	if !entries[0].SettledAt.Equal(first) {
		t.Fatalf("settledAt moved to %v; the first settling must win", entries[0].SettledAt)
	}
}

// TestDuplicateOpeningDoesNotFork: a replayed opening must update nothing and
// fork nothing, or one execution becomes two.
func TestDuplicateOpeningDoesNotFork(t *testing.T) {
	path := sessionPath(t)
	if err := Open(path, Opening{ID: "call/item-1", Name: "first"}); err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := Open(path, Opening{ID: "call/item-1", Name: "second"}); err != nil {
		t.Fatalf("reopen: %v", err)
	}
	entries := History(path)
	if len(entries) != 1 || entries[0].Name != "first" {
		t.Fatalf("entries = %+v, want the first opening only", entries)
	}
}

// TestTornTailIsSkipped: a crash mid-write is what this file exists to survive,
// so a partial last line must not lose the records before it.
func TestTornTailIsSkipped(t *testing.T) {
	path := sessionPath(t)
	if err := Open(path, Opening{ID: "call/item-1"}); err != nil {
		t.Fatalf("open: %v", err)
	}
	f, err := os.OpenFile(store.SessionExecution(path), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open journal: %v", err)
	}
	if _, err := f.WriteString(`{"execution":"call/item-2","stat`); err != nil {
		t.Fatalf("write torn line: %v", err)
	}
	f.Close()
	if entries := History(path); len(entries) != 1 || entries[0].ID != "call/item-1" {
		t.Fatalf("entries = %+v, want the intact record only", entries)
	}
}

// TestNoSessionPathRecordsNothing: a headless run persists nothing, so there is
// nowhere to record to and that must not be an error.
func TestNoSessionPathRecordsNothing(t *testing.T) {
	if err := Open("", Opening{ID: "call/item-1"}); err != nil {
		t.Fatalf("open with no session: %v", err)
	}
	if got := History(""); got != nil {
		t.Fatalf("history = %+v, want nil", got)
	}
}

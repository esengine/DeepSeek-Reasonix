package execjournal

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/state/store"
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

// TestAdoptedOpeningIsClosedOnArrival holds the one disposition that never
// executes. An adopted item has no owner to go missing, so reporting it as
// interrupted would invent work nobody did.
func TestAdoptedOpeningIsClosedOnArrival(t *testing.T) {
	path := sessionPath(t)
	if err := Open(path, Opening{ID: "call/item-1", Disposition: DispositionAdopted, AdoptedFrom: "sa_source"}); err != nil {
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

// TestStartedTellsTheTwoInterruptionsApart is the whole point of the transition.
// Before it existed, an item cut mid-execution and one still waiting on a
// dependency reached the next process as the same fact.
func TestStartedTellsTheTwoInterruptionsApart(t *testing.T) {
	path := sessionPath(t)
	for _, id := range []string{"call/ran", "call/waited"} {
		if err := Open(path, Opening{ID: id}); err != nil {
			t.Fatalf("open %s: %v", id, err)
		}
	}
	if err := Start(path, "call/ran"); err != nil {
		t.Fatalf("start: %v", err)
	}
	Disown(path)

	got := map[string]string{}
	for _, e := range Interrupted(path) {
		got[e.ID] = e.Interruption()
	}
	if got["call/ran"] != InterruptedDuringExecution {
		t.Errorf("started item = %q, want %q", got["call/ran"], InterruptedDuringExecution)
	}
	if got["call/waited"] != InterruptedBeforeStart {
		t.Errorf("unstarted item = %q, want %q", got["call/waited"], InterruptedBeforeStart)
	}
}

// TestAdoptionMustNameItsSource holds one opening to one story. An adoption
// with nobody to have adopted from, or a source on an item that is going to
// run, are two fields disagreeing about what this item is.
func TestAdoptionMustNameItsSource(t *testing.T) {
	for name, tc := range map[string]struct {
		opening Opening
		wantErr bool
	}{
		"adopted with a source": {Opening{ID: "call/a", Disposition: DispositionAdopted, AdoptedFrom: "sa_x"}, false},
		"adopted with none":     {Opening{ID: "call/b", Disposition: DispositionAdopted}, true},
		"pending with a source": {Opening{ID: "call/c", AdoptedFrom: "sa_x"}, true},
		"pending with none":     {Opening{ID: "call/d"}, false},
	} {
		t.Run(name, func(t *testing.T) {
			path := sessionPath(t)
			err := Open(path, tc.opening)
			if tc.wantErr != (err != nil) {
				t.Fatalf("open err = %v, want error: %v", err, tc.wantErr)
			}
			if tc.wantErr && len(History(path)) != 0 {
				t.Fatal("a refused opening must leave no record")
			}
		})
	}
}

// TestOlderAdoptionWithoutASourceIsReadable: an adoption recorded before the
// source was captured is lossy history, not corruption. Refusing to read it
// would condemn sidecars written by a version that could not know better, and
// no other field can be read to guess what it adopted.
func TestOlderAdoptionWithoutASourceIsReadable(t *testing.T) {
	path := sessionPath(t)
	f, err := os.Create(store.SessionExecution(path))
	if err != nil {
		t.Fatalf("create journal: %v", err)
	}
	if _, err := f.WriteString(`{"execution":"call/a","status":"open","disposition":"adopted","at":"2026-01-01T00:00:00Z"}` + "\n"); err != nil {
		t.Fatalf("write: %v", err)
	}
	f.Close()

	entries := History(path)
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	if entries[0].Disposition != DispositionAdopted || entries[0].AdoptedFrom != "" {
		t.Fatalf("entry = %+v, want an adoption whose source is unknown", entries[0])
	}
	if entries[0].Open() {
		t.Fatal("an adopted entry is closed on arrival, whatever its source")
	}
}

// TestWorkerSpecKeepsItsPresence: an entry that recorded the worker layer and
// one written before it existed both read back with empty fields, and they mean
// different things — "this layer named nothing" against "nobody wrote it down".
// Only presence separates them, and a reader that lost it would report a legacy
// entry as an exact reconstruction of an inheritance it never saw.
func TestWorkerSpecKeepsItsPresence(t *testing.T) {
	t.Run("recorded, naming nothing", func(t *testing.T) {
		path := sessionPath(t)
		if err := Open(path, Opening{ID: "call/a", Worker: &WorkerSpec{}}); err != nil {
			t.Fatalf("open: %v", err)
		}
		got := History(path)[0]
		if got.Worker == nil {
			t.Fatal("worker spec lost its presence; empty is a fact here")
		}
		if got.Worker.Model != "" || got.Worker.Effort != "" {
			t.Fatalf("worker = %+v, want both empty", *got.Worker)
		}
	})
	t.Run("recorded, naming an effort", func(t *testing.T) {
		path := sessionPath(t)
		if err := Open(path, Opening{ID: "call/a", Worker: &WorkerSpec{Effort: "low"}}); err != nil {
			t.Fatalf("open: %v", err)
		}
		got := History(path)[0]
		if got.Worker == nil || got.Worker.Model != "" || got.Worker.Effort != "low" {
			t.Fatalf("worker = %+v, want an inherited model and a named effort", got.Worker)
		}
	})
	t.Run("never recorded", func(t *testing.T) {
		path := sessionPath(t)
		f, err := os.Create(store.SessionExecution(path))
		if err != nil {
			t.Fatalf("create journal: %v", err)
		}
		if _, err := f.WriteString(`{"execution":"call/a","status":"open","at":"2026-01-01T00:00:00Z"}` + "\n"); err != nil {
			t.Fatalf("write: %v", err)
		}
		f.Close()
		if got := History(path)[0]; got.Worker != nil {
			t.Fatalf("worker = %+v, want nil: this entry predates the record", *got.Worker)
		}
	})
}

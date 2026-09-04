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

// TestSettlingBeforeStartingIsOrdinary: a branch its dependency cut is opened,
// never started, and released when the group ends. Treating that as corruption
// would reject a path production reaches on every failed fan-out.
func TestSettlingBeforeStartingIsOrdinary(t *testing.T) {
	path := sessionPath(t)
	if err := Open(path, Opening{ID: "call/skipped"}); err != nil {
		t.Fatalf("open: %v", err)
	}
	Settle(path, "call/skipped")
	entries := History(path)
	if len(entries) != 1 || entries[0].Started() || entries[0].Open() {
		t.Fatalf("entries = %+v, want one settled entry that never started", entries)
	}
}

// TestStartRecordsTheJournalRefuses covers the three shapes that would let a
// replayed or torn log describe an execution that never happened that way.
func TestStartRecordsTheJournalRefuses(t *testing.T) {
	t.Run("without an opening", func(t *testing.T) {
		path := sessionPath(t)
		if err := Start(path, "call/ghost"); err != nil {
			t.Fatalf("start: %v", err)
		}
		if entries := History(path); len(entries) != 0 {
			t.Fatalf("entries = %+v, want none from a start with no opening", entries)
		}
	})
	t.Run("after settling", func(t *testing.T) {
		path := sessionPath(t)
		if err := Open(path, Opening{ID: "call/item"}); err != nil {
			t.Fatalf("open: %v", err)
		}
		Settle(path, "call/item")
		if err := Start(path, "call/item"); err != nil {
			t.Fatalf("start: %v", err)
		}
		if entries := History(path); entries[0].Started() {
			t.Fatal("an execution the orchestration released cannot begin afterwards")
		}
	})
	t.Run("twice", func(t *testing.T) {
		path := sessionPath(t)
		if err := Open(path, Opening{ID: "call/item"}); err != nil {
			t.Fatalf("open: %v", err)
		}
		if err := Start(path, "call/item"); err != nil {
			t.Fatalf("start: %v", err)
		}
		first := History(path)[0].StartedAt
		if err := Start(path, "call/item"); err != nil {
			t.Fatalf("restart: %v", err)
		}
		if !History(path)[0].StartedAt.Equal(first) {
			t.Fatal("startedAt moved; the first start must win")
		}
	})
}

// TestLifecycleTruthTable walks every shape an execution can leave behind. The
// rows are not variations on one case: an item blocked by a dependency never
// reaches the scheduler, one the scheduler refused did, and one that settles
// after being refused keeps that provenance even though it never ran.
func TestLifecycleTruthTable(t *testing.T) {
	for name, tc := range map[string]struct {
		queue, start, settle bool
		cause                string
		wantQueued           bool
		wantStarted          bool
		wantCause            string
	}{
		"blocked by a dependency":    {},
		"refused on slots":           {queue: true, cause: "slots", wantQueued: true, wantCause: "slots"},
		"refused on writers":         {queue: true, cause: "writers", wantQueued: true, wantCause: "writers"},
		"refused on a claim":         {queue: true, cause: "claim", wantQueued: true, wantCause: "claim"},
		"admitted at once":           {start: true, wantStarted: true},
		"refused, then admitted":     {queue: true, cause: "slots", start: true, wantQueued: true, wantStarted: true, wantCause: "slots"},
		"refused, settles unstarted": {queue: true, cause: "slots", settle: true, wantQueued: true, wantCause: "slots"},
	} {
		t.Run(name, func(t *testing.T) {
			path := sessionPath(t)
			if err := Open(path, Opening{ID: "call/item"}); err != nil {
				t.Fatalf("open: %v", err)
			}
			if tc.queue {
				if err := Queue(path, "call/item", tc.cause); err != nil {
					t.Fatalf("queue: %v", err)
				}
			}
			if tc.start {
				if err := Start(path, "call/item"); err != nil {
					t.Fatalf("start: %v", err)
				}
			}
			if tc.settle {
				Settle(path, "call/item")
			}
			got := History(path)[0]
			if got.Queued() != tc.wantQueued || got.Started() != tc.wantStarted || got.Cause != tc.wantCause {
				t.Fatalf("entry queued=%v started=%v cause=%q, want %v/%v/%q",
					got.Queued(), got.Started(), got.Cause, tc.wantQueued, tc.wantStarted, tc.wantCause)
			}
		})
	}
}

// TestQueueRecordsTheJournalRefuses holds the cause to the one that queued the
// request. The blocker can change while an item waits — measured: a run kept
// reporting slots after slots had freed — so a later cause must not rewrite it,
// and a queue record must not arrive after the item has moved past waiting.
func TestQueueRecordsTheJournalRefuses(t *testing.T) {
	t.Run("first cause wins", func(t *testing.T) {
		path := sessionPath(t)
		if err := Open(path, Opening{ID: "call/item"}); err != nil {
			t.Fatalf("open: %v", err)
		}
		if err := Queue(path, "call/item", "slots"); err != nil {
			t.Fatalf("queue: %v", err)
		}
		if err := Queue(path, "call/item", "writers"); err != nil {
			t.Fatalf("requeue: %v", err)
		}
		if got := History(path)[0].Cause; got != "slots" {
			t.Fatalf("cause = %q, want the one that queued the request", got)
		}
	})
	t.Run("after starting", func(t *testing.T) {
		path := sessionPath(t)
		if err := Open(path, Opening{ID: "call/item"}); err != nil {
			t.Fatalf("open: %v", err)
		}
		if err := Start(path, "call/item"); err != nil {
			t.Fatalf("start: %v", err)
		}
		if err := Queue(path, "call/item", "slots"); err != nil {
			t.Fatalf("queue: %v", err)
		}
		if History(path)[0].Queued() {
			t.Fatal("an execution already running cannot begin waiting")
		}
	})
	t.Run("after settling", func(t *testing.T) {
		path := sessionPath(t)
		if err := Open(path, Opening{ID: "call/item"}); err != nil {
			t.Fatalf("open: %v", err)
		}
		Settle(path, "call/item")
		if err := Queue(path, "call/item", "slots"); err != nil {
			t.Fatalf("queue: %v", err)
		}
		if History(path)[0].Queued() {
			t.Fatal("an execution the orchestration released cannot begin waiting")
		}
	})
	t.Run("without an opening", func(t *testing.T) {
		path := sessionPath(t)
		if err := Queue(path, "call/ghost", "slots"); err != nil {
			t.Fatalf("queue: %v", err)
		}
		if entries := History(path); len(entries) != 0 {
			t.Fatalf("entries = %+v, want none from a queue with no opening", entries)
		}
	})
}

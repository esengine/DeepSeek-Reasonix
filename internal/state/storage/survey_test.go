package storage

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
)

func write(t *testing.T, path string, size int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, make([]byte, size), 0o600); err != nil {
		t.Fatal(err)
	}
}

func find(t *testing.T, roots []Root, id config.RootID) Root {
	t.Helper()
	for _, r := range roots {
		if r.ID == id {
			return r
		}
	}
	t.Fatalf("root %q missing from the survey", id)
	return Root{}
}

// The survey reads the root table, so every declared root appears without this
// package being told the set — that is what keeps a newly declared root from
// silently going unaccounted for.
func TestSurveyCoversEveryDeclaredRoot(t *testing.T) {
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("REASONIX_STATE_HOME", "")
	t.Setenv("REASONIX_CACHE_HOME", "")

	roots := Survey(t.Context())
	if len(roots) != len(config.RootIDs()) {
		t.Fatalf("survey has %d roots, table declares %d", len(roots), len(config.RootIDs()))
	}
	for _, id := range config.RootIDs() {
		find(t, roots, id)
	}
}

// Bytes and files are counted, not estimated: the number is what a user decides
// to delete on.
func TestSurveyCountsWhatIsActuallyThere(t *testing.T) {
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("REASONIX_STATE_HOME", "")

	write(t, filepath.Join(home, "sessions", "a.jsonl"), 1200)
	write(t, filepath.Join(home, "sessions", "a.events.jsonl"), 800)
	write(t, filepath.Join(home, "archive", "old.jsonl"), 4000)

	state := find(t, Survey(t.Context()), config.RootState)
	if state.Bytes != 6000 || state.Files != 3 {
		t.Fatalf("state = %d bytes / %d files, want 6000 / 3", state.Bytes, state.Files)
	}
	if state.Missing || state.Err != "" {
		t.Fatalf("state reported missing=%v err=%q", state.Missing, state.Err)
	}
}

// A root nothing has written yet is not a failure. A fresh install has no
// worktrees, and zero is the honest answer rather than an error.
func TestNeverWrittenRootReportsMissingRatherThanFailing(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	t.Setenv("REASONIX_STATE_HOME", filepath.Join(testenv.TempDir(t), "never-created"))

	state := find(t, Survey(t.Context()), config.RootState)
	if !state.Missing {
		t.Fatalf("unwritten root reported missing=%v", state.Missing)
	}
	if state.Bytes != 0 || state.Err != "" {
		t.Fatalf("unwritten root = %d bytes, err %q", state.Bytes, state.Err)
	}
}

// The survey carries whether a root may be moved and what pins it, so a
// surface offering relocation never has to decide that for itself.
func TestSurveyCarriesRelocationFacts(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	pinned := testenv.TempDir(t)
	t.Setenv("REASONIX_STATE_HOME", pinned)

	roots := Survey(t.Context())
	state := find(t, roots, config.RootState)
	if !state.Relocatable || state.PinnedBy != "REASONIX_STATE_HOME" {
		t.Fatalf("state relocatable=%v pinnedBy=%q", state.Relocatable, state.PinnedBy)
	}
	locks := find(t, roots, config.RootLocks)
	if locks.Relocatable {
		t.Fatal("the locks root must never report itself movable")
	}
}

// Free space is what decides whether a move fits, so a surveyed root has to
// carry the volume under it.
func TestSurveyReadsTheVolumeUnderEachRoot(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	t.Setenv("REASONIX_STATE_HOME", "")

	state := find(t, Survey(t.Context()), config.RootState)
	if state.Volume.Total <= 0 || state.Volume.Free <= 0 {
		t.Fatalf("volume = %+v, want a real free/total pair", state.Volume)
	}
	if state.Volume.Free > state.Volume.Total {
		t.Fatalf("volume free %d exceeds total %d", state.Volume.Free, state.Volume.Total)
	}
}

// A cancelled survey stops where it is and says so, rather than running a walk
// nobody is waiting for any more.
func TestCancelledSurveyStops(t *testing.T) {
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("REASONIX_STATE_HOME", "")
	write(t, filepath.Join(home, "sessions", "a.jsonl"), 10)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	state := find(t, Survey(ctx), config.RootState)
	if state.Err == "" {
		t.Fatal("a cancelled walk must report why it stopped")
	}
}

// stallAfterWalk stands in for a filesystem whose walk reports what it found
// and then never comes back, the way a huge or network-mounted tree does.
func stallAfterWalk(t *testing.T) {
	t.Helper()
	release := make(chan struct{})
	real := walkDir
	walkDir = func(root string, fn fs.WalkDirFunc) error {
		err := real(root, fn)
		<-release
		return err
	}
	t.Cleanup(func() {
		close(release)
		walkDir = real
	})
}

func TestSurveyReturnsPartialCountWhenAWalkStalls(t *testing.T) {
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("REASONIX_STATE_HOME", "")
	write(t, filepath.Join(home, "sessions", "a.jsonl"), 1200)
	stallAfterWalk(t)
	old := Budget
	Budget = 50 * time.Millisecond
	t.Cleanup(func() { Budget = old })

	done := make(chan []Root, 1)
	go func() { done <- Survey(t.Context()) }()
	select {
	case roots := <-done:
		state := find(t, roots, config.RootState)
		if !state.Truncated {
			t.Fatalf("a stalled walk must report Truncated, got %+v", state)
		}
		if state.Bytes != 1200 || state.Files != 1 {
			t.Fatalf("the partial count must survive: %d bytes, %d files", state.Bytes, state.Files)
		}
		if state.Err != "" {
			t.Fatalf("a spent budget is Truncated, not an error: %q", state.Err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Survey blocked on a stalled walk")
	}
}

func TestSurveyRootDoesNotWaitForOtherRoots(t *testing.T) {
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("REASONIX_STATE_HOME", "")
	write(t, filepath.Join(home, "sessions", "a.jsonl"), 10)
	root, ok := SurveyRoot(t.Context(), config.RootState)
	if !ok || root.Bytes != 10 || root.Truncated {
		t.Fatalf("SurveyRoot = %+v ok=%v", root, ok)
	}
	if _, ok := SurveyRoot(t.Context(), config.RootID("nope")); ok {
		t.Fatal("an undeclared root must not be measured")
	}
}

func TestLayoutReadsNothing(t *testing.T) {
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("REASONIX_STATE_HOME", "")
	write(t, filepath.Join(home, "sessions", "a.jsonl"), 10)
	stallAfterWalk(t)
	roots := Layout()
	if len(roots) != len(config.RootIDs()) {
		t.Fatalf("layout has %d roots", len(roots))
	}
	state := find(t, roots, config.RootState)
	if !state.Pending || state.Bytes != 0 || state.Dir == "" {
		t.Fatalf("layout root = %+v", state)
	}
}

func TestTruncatedRootKeepsTheErrorFoundBeforeTheBudgetRanOut(t *testing.T) {
	dir := testenv.TempDir(t)
	write(t, filepath.Join(dir, "a"), 5)
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	old := walkDir
	walkDir = func(root string, fn fs.WalkDirFunc) error {
		_ = fn(root, nil, fs.ErrPermission)
		<-release
		return nil
	}
	t.Cleanup(func() { walkDir = old })
	_, _, _, truncated, errText := measureRoot(t.Context(), config.RootID("x"), dir, 50*time.Millisecond)
	if !truncated || errText == "" {
		t.Fatalf("truncated=%v err=%q: both must be reported", truncated, errText)
	}
}

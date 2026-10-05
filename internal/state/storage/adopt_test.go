package storage

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
)

// The report this exists for: a Studio whose configuration came back without
// the relocation still had every byte of it on the other drive, and the only
// way back was refused because that folder was — correctly — not empty. A
// folder that holds this root's own data is not a stranger's folder.
func TestAFolderHoldingThisRootIsPointedAtRatherThanRefused(t *testing.T) {
	home := stateHome(t)
	seedState(t, home)
	target := filepath.Join(testenv.TempDir(t), "moved")

	first := PlanMove(t.Context(), config.RootState, target)
	if err := Move(t.Context(), first, nil); err != nil {
		t.Fatalf("move: %v", err)
	}

	// What losing the pointer looks like: the data is still there, the
	// configuration no longer names it.
	if err := commitLocation(config.RootState, ""); err != nil {
		t.Fatalf("clear the configured location: %v", err)
	}
	if got := config.RootDir(config.RootState); got != home {
		t.Fatalf("state = %q after the pointer was lost, want the default %q", got, home)
	}

	back := PlanMove(t.Context(), config.RootState, target)
	if !back.OK() {
		t.Fatalf("pointing back at the data was refused: %+v", back.Refusals)
	}
	if !back.Adopt {
		t.Fatal("the plan proposed to copy data that is already there")
	}
	if back.Bytes == 0 || back.Files == 0 {
		t.Fatalf("the plan describes nothing to adopt: %d bytes in %d files", back.Bytes, back.Files)
	}
	var phases []Phase
	if err := Move(t.Context(), back, func(p Progress) { phases = append(phases, p.Phase) }); err != nil {
		t.Fatalf("adopt: %v", err)
	}
	if got := config.RootDir(config.RootState); got != target {
		t.Fatalf("state = %q after the adopt, want %q", got, target)
	}
	if _, err := os.Stat(filepath.Join(target, "sessions", "a.jsonl")); err != nil {
		t.Fatalf("the adopted transcripts are gone: %v", err)
	}
	if !slices.Contains(phases, PhaseAdopting) {
		t.Fatalf("phases = %v, want the adopt to say what it was doing", phases)
	}
}

// An adopt copies nothing and deletes nothing, so whatever the runtime wrote at
// the default location while the pointer was lost is still there afterwards —
// and the plan says how much of it there is rather than letting it go quietly.
func TestAnAdoptLeavesTheOldLocationAloneAndSaysSo(t *testing.T) {
	home := stateHome(t)
	target := filepath.Join(testenv.TempDir(t), "moved")
	write(t, filepath.Join(target, "sessions", "old.jsonl"), 4096)
	seedState(t, home)

	plan := PlanMove(t.Context(), config.RootState, target)
	if !plan.Adopt {
		t.Fatalf("a folder holding only this root's entries was not adopted: %+v", plan.Refusals)
	}
	if plan.Stays == 0 {
		t.Fatal("the plan does not report what stays at the current location")
	}
	if err := Move(t.Context(), plan, nil); err != nil {
		t.Fatalf("adopt: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, "sessions", "a.jsonl")); err != nil {
		t.Fatalf("the adopt deleted what was at the old location: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "sessions", "old.jsonl")); err != nil {
		t.Fatalf("the adopt disturbed the data it adopted: %v", err)
	}
}

// A root with sole occupancy declares no entries, so nothing but the marker can
// tell its folder from any other. A move writes one; that is what makes the
// cache and the worktrees adoptable too.
func TestAMarkedFolderIsAdoptableForARootThatDeclaresNoEntries(t *testing.T) {
	home := stateHome(t)
	write(t, filepath.Join(home, "cache", "catalog.json"), 2048)
	t.Setenv("REASONIX_CACHE_HOME", "")
	if err := commitLocation(config.RootCache, filepath.Join(home, "cache")); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(testenv.TempDir(t), "moved-cache")

	plan := PlanMove(t.Context(), config.RootCache, target)
	if !plan.OK() {
		t.Fatalf("plan refused: %+v", plan.Refusals)
	}
	if err := Move(t.Context(), plan, nil); err != nil {
		t.Fatalf("move: %v", err)
	}
	marker, ok := ReadMarker(target)
	if !ok || marker.Root != config.RootCache {
		t.Fatalf("the moved cache does not say what it is: %+v (found=%v)", marker, ok)
	}

	if err := commitLocation(config.RootCache, ""); err != nil {
		t.Fatal(err)
	}
	back := PlanMove(t.Context(), config.RootCache, target)
	if !back.Adopt {
		t.Fatalf("a folder carrying this root's marker was not adopted: %+v", back.Refusals)
	}
}

// The marker names a location, so it must never travel with the data: one at a
// destination has to mean the copy that landed there finished, and one at a
// place the root has left would claim to hold what is no longer in it.
func TestTheMarkerStaysWithTheLocationRatherThanTheData(t *testing.T) {
	home := stateHome(t)
	seedState(t, home)
	first := filepath.Join(testenv.TempDir(t), "first")
	second := filepath.Join(testenv.TempDir(t), "second")

	if err := Move(t.Context(), PlanMove(t.Context(), config.RootState, first), nil); err != nil {
		t.Fatalf("first move: %v", err)
	}
	if err := Move(t.Context(), PlanMove(t.Context(), config.RootState, second), nil); err != nil {
		t.Fatalf("second move: %v", err)
	}
	if _, ok := ReadMarker(first); ok {
		t.Fatal("the vacated folder still claims to hold this root")
	}
	if marker, ok := ReadMarker(second); !ok || marker.Root != config.RootState {
		t.Fatalf("the current folder does not claim the root: %+v (found=%v)", marker, ok)
	}
	if _, err := os.Stat(filepath.Join(second, markerName)); err != nil {
		t.Fatalf("marker missing at the destination: %v", err)
	}
	if entries, err := os.ReadDir(filepath.Join(second, "sessions")); err == nil {
		for _, entry := range entries {
			if entry.Name() == markerName {
				t.Fatal("the marker was copied into the data it describes")
			}
		}
	}
}

// A folder left holding nothing but the marker of a root that has moved on is
// an empty target, not one to adopt: adopting it would point the runtime at
// data that is not there.
func TestAFolderWithOnlyAStaleMarkerIsTreatedAsEmpty(t *testing.T) {
	home := stateHome(t)
	seedState(t, home)
	target := filepath.Join(testenv.TempDir(t), "stale")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := WriteMarker(target, config.RootState, ""); err != nil {
		t.Fatal(err)
	}

	plan := PlanMove(t.Context(), config.RootState, target)
	if !plan.OK() {
		t.Fatalf("plan refused: %+v", plan.Refusals)
	}
	if plan.Adopt {
		t.Fatal("a folder holding no data was adopted on the strength of a stale marker")
	}
	if err := Move(t.Context(), plan, nil); err != nil {
		t.Fatalf("move: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "sessions", "a.jsonl")); err != nil {
		t.Fatalf("the data did not move: %v", err)
	}
}

// defaultRoots puts the OS defaults in a temp tree and clears both redirects,
// so what runs is the path a real install takes.
func defaultRoots(t *testing.T) string {
	t.Helper()
	base := testenv.TempDir(t)
	t.Setenv("HOME", base)
	t.Setenv("USERPROFILE", base)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(base, ".config"))
	t.Setenv("AppData", filepath.Join(base, "AppData"))
	t.Setenv("REASONIX_HOME", "")
	t.Setenv("REASONIX_STATE_HOME", "")
	t.Setenv("REASONIX_CACHE_HOME", "")
	t.Cleanup(config.InvalidateStorageDirs)
	config.InvalidateStorageDirs()
	return base
}

// A relocation performed before markers existed left nothing in the folder it
// created. The launch that finds it configured records it, so the folder can be
// pointed at again if that configuration is ever lost.
func TestALocationTheConfigurationAlreadyChoseGetsMarked(t *testing.T) {
	base := defaultRoots(t)
	moved := filepath.Join(base, "moved-by-an-older-build")
	write(t, filepath.Join(moved, "sessions", "a.jsonl"), 1024)
	if err := commitLocation(config.RootState, moved); err != nil {
		t.Fatal(err)
	}
	if _, ok := ReadMarker(moved); ok {
		t.Fatal("the folder was marked before anything ran")
	}

	RecordRelocatedRoots()

	marker, ok := ReadMarker(moved)
	if !ok || marker.Root != config.RootState {
		t.Fatalf("the configured location was not marked: %+v (found=%v)", marker, ok)
	}
	// The default locations are shared, so nothing may claim them: home would
	// then read as the state root, and the lock directory as the cache.
	if _, ok := ReadMarker(config.ReasonixHomeDir()); ok {
		t.Fatal("a root sitting at its default claimed the folder it shares")
	}
}

// A run whose roots the environment redirected is a scratch run, and this one
// writes into the directory the production data lives in — the same rule the
// relocation importers follow.
func TestTheBackfillSkipsWhenTheRootsAreRedirectedForThisRun(t *testing.T) {
	base := defaultRoots(t)
	moved := filepath.Join(base, "moved-by-an-older-build")
	write(t, filepath.Join(moved, "sessions", "a.jsonl"), 1024)
	if err := commitLocation(config.RootState, moved); err != nil {
		t.Fatal(err)
	}
	t.Setenv("REASONIX_STATE_HOME", filepath.Join(base, "scratch"))
	config.InvalidateStorageDirs()

	RecordRelocatedRoots()

	if _, ok := ReadMarker(moved); ok {
		t.Fatal("a redirected run wrote into the production location")
	}
}

// Structure decides, not resemblance: a folder that holds anything this root
// does not own is somebody else's, however much of it looks familiar.
func TestAFolderWithAnythingElseInItIsNotClaimed(t *testing.T) {
	stateHome(t)
	dir := testenv.TempDir(t)
	for _, name := range []string{"sessions", "holiday-photos"} {
		if err := os.MkdirAll(filepath.Join(dir, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if holdsRoot(config.RootState, dir, entries) {
		t.Fatal("a folder with unrelated directories in it was claimed as this root")
	}
}

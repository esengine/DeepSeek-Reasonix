package storage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
)

func stateHome(t *testing.T) string {
	t.Helper()
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("REASONIX_STATE_HOME", "")
	t.Setenv("REASONIX_CACHE_HOME", "")
	t.Cleanup(config.InvalidateStorageDirs)
	config.InvalidateStorageDirs()
	return home
}

func seedState(t *testing.T, home string) {
	t.Helper()
	write(t, filepath.Join(home, "sessions", "a.jsonl"), 4096)
	write(t, filepath.Join(home, "sessions", "a.events.jsonl"), 2048)
	write(t, filepath.Join(home, "archive", "old.jsonl"), 1024)
}

// The whole point of the sequence: the copy is proved, the configuration then
// points at it, and the original is only reclaimed after both.
func TestMoveCopiesVerifiesCommitsAndReclaims(t *testing.T) {
	home := stateHome(t)
	seedState(t, home)
	target := filepath.Join(testenv.TempDir(t), "moved")

	plan := PlanMove(t.Context(), config.RootState, target)
	if !plan.OK() {
		t.Fatalf("plan refused: %+v", plan.Refusals)
	}
	var phases []Phase
	if err := Move(t.Context(), plan, func(p Progress) {
		if len(phases) == 0 || phases[len(phases)-1] != p.Phase {
			phases = append(phases, p.Phase)
		}
	}); err != nil {
		t.Fatalf("move: %v", err)
	}

	if got := config.RootDir(config.RootState); got != target {
		t.Fatalf("state resolves to %q after the move, want %q", got, target)
	}
	if _, err := os.Stat(filepath.Join(target, "sessions", "a.jsonl")); err != nil {
		t.Fatalf("moved transcript missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, "sessions")); !os.IsNotExist(err) {
		t.Fatalf("source survived the move: %v", err)
	}
	if len(phases) == 0 || phases[len(phases)-1] != PhaseDone {
		t.Fatalf("phases = %v, want it to end at done", phases)
	}
}

// Before the commit nothing has changed. A cancelled move must leave the
// runtime reading exactly what it read before, whatever bytes were copied.
func TestCancelBeforeCommitChangesNothing(t *testing.T) {
	home := stateHome(t)
	seedState(t, home)
	target := filepath.Join(testenv.TempDir(t), "moved")

	plan := PlanMove(t.Context(), config.RootState, target)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := Move(ctx, plan, nil)
	if err == nil {
		t.Fatal("a cancelled move reported success")
	}
	if !errors.Is(err, ErrMoveInterrupted) && !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled move failed with %v", err)
	}
	config.InvalidateStorageDirs()
	if got := config.RootDir(config.RootState); got != home {
		t.Fatalf("state moved to %q despite the cancel, want %q", got, home)
	}
	if _, err := os.Stat(filepath.Join(home, "sessions", "a.jsonl")); err != nil {
		t.Fatalf("source was touched before the commit: %v", err)
	}
}

// A target holding a stranger's files is refused — unless the journal says an
// interrupted move of this very root put them there, which is what makes the
// copy restartable without letting it merge into somebody's documents.
func TestOnlyAJournalledMoveMayResumeIntoAStrangersFolder(t *testing.T) {
	home := stateHome(t)
	seedState(t, home)
	target := filepath.Join(testenv.TempDir(t), "moved")
	write(t, filepath.Join(target, "tax-returns", "2025.pdf"), 4096)

	plan := PlanMove(t.Context(), config.RootState, target)
	if plan.OK() {
		t.Fatal("a target with someone else's files in it was accepted without a journal")
	}
	if !slices.Contains(refusalCodes(plan), "target.not_empty") {
		t.Fatalf("refusals = %v, want target.not_empty", refusalCodes(plan))
	}

	// The journal an interrupted first run would have left.
	if err := writeJournal(Journal{
		Root: config.RootState, From: home, To: target, Phase: PhaseCopying,
	}); err != nil {
		t.Fatal(err)
	}
	resumed := PlanMove(t.Context(), config.RootState, target)
	if !resumed.OK() {
		t.Fatalf("the journalled move was refused: %+v", resumed.Refusals)
	}
	if err := Move(t.Context(), resumed, nil); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if got := config.RootDir(config.RootState); got != target {
		t.Fatalf("state = %q after the resume, want %q", got, target)
	}
}

// A move only takes what its root owns. State shares a folder with home on a
// default Windows install, so a move of the transcripts must leave the
// configuration and the credentials exactly where they were.
func TestMovingASharedRootLeavesItsNeighbourAlone(t *testing.T) {
	home := stateHome(t)
	seedState(t, home)
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte("default_model = \"a/b\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".env"), []byte("KEY=secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(testenv.TempDir(t), "moved")

	plan := PlanMove(t.Context(), config.RootState, target)
	if err := Move(t.Context(), plan, nil); err != nil {
		t.Fatalf("move: %v", err)
	}
	for _, name := range []string{"config.toml", ".env"} {
		if _, err := os.Stat(filepath.Join(home, name)); err != nil {
			t.Fatalf("%s left home during a state move: %v", name, err)
		}
		if _, err := os.Stat(filepath.Join(target, name)); !os.IsNotExist(err) {
			t.Fatalf("%s was carried into the state target", name)
		}
	}
	if _, err := os.Stat(filepath.Join(target, "sessions", "a.jsonl")); err != nil {
		t.Fatalf("the transcripts did not move: %v", err)
	}
}

func refusalCodes(p Plan) []string {
	var out []string
	for _, r := range p.Refusals {
		out = append(out, r.Code)
	}
	return out
}

// Every refusal the preflight can see is reported at once: a person choosing a
// folder should not have to discover them one attempt at a time.
func TestPreflightReportsEveryReasonItCanSee(t *testing.T) {
	home := stateHome(t)
	seedState(t, home)

	inside := filepath.Join(home, "sessions")
	plan := PlanMove(t.Context(), config.RootState, inside)
	if plan.OK() {
		t.Fatal("moving a root into itself was allowed")
	}
	for _, r := range plan.Refusals {
		if strings.TrimSpace(r.Detail) == "" {
			t.Fatalf("refusal %q carries no sentence for a reader", r.Code)
		}
	}
	if !slices.Contains(refusalCodes(plan), "target.inside_source") {
		t.Fatalf("refusals = %v, want target.inside_source", refusalCodes(plan))
	}
}

// The immovable roots are refused by the same preflight the panel calls, so a
// surface cannot route around the contract by asking for a move directly.
func TestPreflightRefusesTheImmovableRoots(t *testing.T) {
	stateHome(t)
	for _, id := range []config.RootID{config.RootHome, config.RootLocks} {
		plan := PlanMove(t.Context(), id, testenv.TempDir(t))
		if plan.OK() {
			t.Fatalf("%s was allowed to move", id)
		}
		if plan.Refusals[0].Code != "root.immovable" {
			t.Fatalf("%s refused with %q", id, plan.Refusals[0].Code)
		}
	}
}

// A root the environment pins is refused with the variable named, rather than
// accepted into a configuration the environment would then override.
func TestPreflightNamesTheVariableThatPinsARoot(t *testing.T) {
	stateHome(t)
	t.Setenv("REASONIX_STATE_HOME", testenv.TempDir(t))
	plan := PlanMove(t.Context(), config.RootState, testenv.TempDir(t))
	if plan.OK() {
		t.Fatal("a pinned root was allowed to move")
	}
	if !strings.Contains(plan.Refusals[0].Detail, "REASONIX_STATE_HOME") {
		t.Fatalf("refusal %q does not name the variable", plan.Refusals[0].Detail)
	}
}

// A move that reached the commit leaves a journal saying so, which is what
// lets a later launch tell "interrupted, nothing happened" from "committed,
// finish the cleanup".
func TestJournalIsClearedOnlyWhenTheMoveIsDone(t *testing.T) {
	home := stateHome(t)
	seedState(t, home)
	target := filepath.Join(testenv.TempDir(t), "moved")

	if _, pending := PendingMove(); pending {
		t.Fatal("a fresh home reports a pending move")
	}
	plan := PlanMove(t.Context(), config.RootState, target)
	if err := Move(t.Context(), plan, nil); err != nil {
		t.Fatalf("move: %v", err)
	}
	if j, pending := PendingMove(); pending {
		t.Fatalf("a finished move left a journal: %+v", j)
	}
}

// The sidebar's remembered project list is a file at the top of the state
// root. A move that carries sessions but not the list leaves every transcript
// on disk and none of them reachable.
func TestMoveCarriesTheRememberedProjectList(t *testing.T) {
	home := stateHome(t)
	seedState(t, home)
	list := filepath.Join(home, "serve-workspaces.json")
	if err := os.WriteFile(list, []byte(`{"paths":["D:\\work\\app"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(testenv.TempDir(t), "moved")

	plan := PlanMove(t.Context(), config.RootState, target)
	if err := Move(t.Context(), plan, nil); err != nil {
		t.Fatalf("move: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "serve-workspaces.json")); err != nil {
		t.Fatalf("project list was not carried to the new root: %v", err)
	}
}

// Every top-level entry the state root owns travels with it: a move that left
// standing instructions or memory behind would read as them being deleted.
func TestMoveCarriesEveryOwnedEntry(t *testing.T) {
	home := stateHome(t)
	for _, name := range config.StateRootEntries {
		if filepath.Ext(name) != "" {
			write(t, filepath.Join(home, name), 8)
		} else {
			write(t, filepath.Join(home, name, "f"), 8)
		}
	}
	target := filepath.Join(testenv.TempDir(t), "moved")
	if err := Move(t.Context(), PlanMove(t.Context(), config.RootState, target), nil); err != nil {
		t.Fatalf("move: %v", err)
	}
	for _, name := range config.StateRootEntries {
		if _, err := os.Lstat(filepath.Join(target, name)); err != nil {
			t.Errorf("%s was not carried: %v", name, err)
		}
	}
}

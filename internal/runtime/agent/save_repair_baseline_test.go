package agent

import (
	"errors"
	"path/filepath"
	"reasonix/internal/state/sessionstore"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/provider"
)

func seedTwoTurnSession(t *testing.T, path string) {
	t.Helper()
	s := sessionstore.NewSession("sys")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "你好"})
	s.Add(provider.Message{Role: provider.RoleAssistant, Content: "hi"})
	if err := s.Save(path); err != nil {
		t.Fatalf("seed: %v", err)
	}
}

// Two readers of one transcript, each appending its own turn, is what divergence
// actually is: neither snapshot contains the other. This pins the mechanism the
// recovery fork exists for, so a change that stops detecting it fails here.
func TestTwoWritersOnOneTranscriptDiverge(t *testing.T) {
	dir := testenv.TempDir(t)
	path := filepath.Join(dir, "session.jsonl")
	seedTwoTurnSession(t, path)

	first, err := sessionstore.LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	second, err := sessionstore.LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}

	first.Add(provider.Message{Role: provider.RoleUser, Content: "写者甲的话"})
	if err := first.SaveSnapshot(path); err != nil {
		t.Fatalf("first writer: %v", err)
	}

	second.Add(provider.Message{Role: provider.RoleUser, Content: "写者乙的话"})
	err = second.SaveSnapshot(path)
	if !errors.Is(err, sessionstore.ErrSessionSnapshotConflict) {
		t.Fatalf("second writer = %v, want a snapshot conflict", err)
	}
	kind, _ := sessionstore.SnapshotConflictKind(err)
	if kind != sessionstore.SessionSnapshotConflictDiverged {
		t.Fatalf("conflict kind = %q, want diverged", kind)
	}
}

// The turn the loser is holding is nowhere else, so it forks — that part is the
// point of the mechanism. What must not happen is a file per incident: the same
// unsaved turn arriving again from another Session belongs in the branch that
// already holds it.
func TestDivergedWritersShareOneRecoveryBranch(t *testing.T) {
	dir := testenv.TempDir(t)
	path := filepath.Join(dir, "session.jsonl")
	seedTwoTurnSession(t, path)

	winner, err := sessionstore.LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	winner.Add(provider.Message{Role: provider.RoleUser, Content: "落盘的那一轮"})
	if err := winner.SaveSnapshot(path); err != nil {
		t.Fatal(err)
	}

	branches := map[string]bool{}
	for i := range 5 {
		loser, err := sessionstore.LoadSession(path)
		if err != nil {
			t.Fatal(err)
		}
		// Every loser holds the same unsaved turn: one conversation reopened,
		// rebuilt or resumed again and again after the same incident.
		loser.Replace(append(seedPrefix(t, path), provider.Message{Role: provider.RoleUser, Content: "没落盘的那一轮"}))
		info, err := loser.SaveRecoveryBranch(sessionstore.RecoveryBranchOptions{OriginalPath: path})
		if err != nil {
			t.Fatalf("loser %d: %v", i, err)
		}
		branches[info.Path] = true
	}
	if len(branches) != 1 {
		t.Fatalf("five incidents over the same unsaved turn produced %d branches, want 1", len(branches))
	}
}

// seedPrefix returns the transcript as it stood before the winner's turn, which
// is the baseline every loser is holding.
func seedPrefix(t *testing.T, path string) []provider.Message {
	t.Helper()
	s := sessionstore.NewSession("sys")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "你好"})
	s.Add(provider.Message{Role: provider.RoleAssistant, Content: "hi"})
	return s.Snapshot()
}

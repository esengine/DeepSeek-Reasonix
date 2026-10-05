package agent

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reasonix/internal/state/sessionstore"
	"reasonix/internal/state/store"
	"slices"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/provider"
)

// staleSessionOver returns a session holding the parent transcript plus one
// unsaved message, which is what a snapshot conflict has in memory.
func staleSessionOver(parent []provider.Message, extra string) *sessionstore.Session {
	s := sessionstore.NewSession("sys")
	for _, m := range parent {
		if m.Role == provider.RoleSystem {
			continue
		}
		s.Add(m)
	}
	s.Add(provider.Message{Role: provider.RoleUser, Content: extra})
	return s
}

// Recovery lanes are per live Session, so a conflict from a fresh Session is a
// fresh file. The depth cap bounds the chain (parent → branch → branch), never
// how many siblings one parent may accumulate — and every resume, rebuild or
// reopened pane brings a new Session. The report that prompted this test was a
// sidebar holding dozens of rows with one title and one turn count.
func TestRepeatedConflictsFromFreshSessionsDoNotFanOut(t *testing.T) {
	dir := testenv.TempDir(t)
	path := filepath.Join(dir, "session.jsonl")
	parent := sessionstore.NewSession("sys")
	parent.Add(provider.Message{Role: provider.RoleUser, Content: "你好"})
	parent.Add(provider.Message{Role: provider.RoleAssistant, Content: "hi"})
	if err := parent.Save(path); err != nil {
		t.Fatalf("save parent: %v", err)
	}
	parentMsgs := parent.Snapshot()

	paths := map[string]bool{}
	for i := range 12 {
		// The same unsaved turns every time: one conversation reopened again
		// and again, which is what puts a dozen identical rows in the sidebar.
		stale := staleSessionOver(parentMsgs, "unsaved")
		info, err := stale.SaveRecoveryBranch(sessionstore.RecoveryBranchOptions{OriginalPath: path})
		if err != nil {
			t.Fatalf("conflict %d: %v", i, err)
		}
		paths[info.Path] = true
	}
	if len(paths) > 1 {
		t.Errorf("12 conflicts over the same content produced %d recovery branches; one parent should not accumulate identical siblings", len(paths))
	}
}

// Collapsing identical content must not collapse different content: a branch
// exists to keep turns that are nowhere else, and two conflicts carrying
// different unsaved work are two things to keep.
func TestDistinctConflictContentStillGetsItsOwnBranch(t *testing.T) {
	dir := testenv.TempDir(t)
	path := filepath.Join(dir, "session.jsonl")
	parent := sessionstore.NewSession("sys")
	parent.Add(provider.Message{Role: provider.RoleUser, Content: "你好"})
	parent.Add(provider.Message{Role: provider.RoleAssistant, Content: "hi"})
	if err := parent.Save(path); err != nil {
		t.Fatalf("save parent: %v", err)
	}
	parentMsgs := parent.Snapshot()

	paths := map[string]bool{}
	for i := range 3 {
		stale := staleSessionOver(parentMsgs, fmt.Sprintf("unsaved %d", i))
		info, err := stale.SaveRecoveryBranch(sessionstore.RecoveryBranchOptions{OriginalPath: path})
		if err != nil {
			t.Fatalf("conflict %d: %v", i, err)
		}
		paths[info.Path] = true
	}
	if len(paths) != 3 {
		t.Errorf("three different unsaved transcripts landed in %d branches; each carries turns the others do not", len(paths))
	}
}

// The same content arriving twice must not become two files: a Session that
// reloads and conflicts again on an unchanged transcript is the common case,
// and it is the one that fills a sidebar fastest.
func TestIdenticalConflictContentReusesOneBranch(t *testing.T) {
	dir := testenv.TempDir(t)
	path := filepath.Join(dir, "session.jsonl")
	parent := sessionstore.NewSession("sys")
	parent.Add(provider.Message{Role: provider.RoleUser, Content: "你好"})
	parent.Add(provider.Message{Role: provider.RoleAssistant, Content: "hi"})
	if err := parent.Save(path); err != nil {
		t.Fatalf("save parent: %v", err)
	}
	parentMsgs := parent.Snapshot()

	first, err := staleSessionOver(parentMsgs, "same").SaveRecoveryBranch(sessionstore.RecoveryBranchOptions{OriginalPath: path})
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := staleSessionOver(parentMsgs, "same").SaveRecoveryBranch(sessionstore.RecoveryBranchOptions{OriginalPath: path})
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if first.Path != second.Path {
		t.Errorf("identical content wrote two branches:\n  %s\n  %s", first.Path, second.Path)
	}
}

// conflictRecoveryTick mirrors control.recoverSnapshotConflict: fork while the
// chain has room, then keep landing in this writer's isolated lane.
func conflictRecoveryTick(t *testing.T, s *sessionstore.Session, path string) sessionstore.RecoveryBranchInfo {
	t.Helper()
	info, err := s.SaveRecoveryBranch(sessionstore.RecoveryBranchOptions{OriginalPath: path})
	if err == nil {
		return info
	}
	if !errors.Is(err, sessionstore.ErrSessionRecoveryDepthExceeded) {
		t.Fatalf("recovery tick on %s: %v", filepath.Base(path), err)
	}
	info, err = s.SaveConflictRecoveryBranch(sessionstore.RecoveryBranchOptions{OriginalPath: path})
	if err != nil {
		t.Fatalf("isolated recovery tick on %s: %v", filepath.Base(path), err)
	}
	return info
}

// The live-turn shape of the fan-out: the canonical file keeps being written
// from outside while one writer works on, so every tick conflicts over content
// that grew since the last — identical-digest reuse cannot collapse those, and
// each tick runs on a new Session. One writer, one lineage, one file. The
// report behind this test gained a sidebar row every 30s for 25 minutes.
func TestGrowingConflictsFromOneWriterStayInOneLane(t *testing.T) {
	dir := testenv.TempDir(t)
	canonical := filepath.Join(dir, "session.jsonl")
	seed := []provider.Message{
		{Role: provider.RoleUser, Content: "你好"},
		{Role: provider.RoleAssistant, Content: "hi"},
	}
	rewriteTranscriptOutsideReasonix(t, canonical, seed...)

	live := append([]provider.Message(nil), seed...)
	target := canonical
	branches := map[string]bool{}
	for i := range 8 {
		live = append(live, provider.Message{Role: provider.RoleAssistant, Content: fmt.Sprintf("turn output %d", i)})
		session := sessionstore.NewSession("sys")
		for _, m := range live {
			session.Add(m)
		}
		rewriteTranscriptOutsideReasonix(t, canonical, append(append([]provider.Message(nil), seed...),
			provider.Message{Role: provider.RoleAssistant, Content: fmt.Sprintf("别处写进来的第 %d 版", i)})...)
		if err := session.SaveSnapshot(target); err == nil {
			continue // the branch already holds this writer's work; nothing to fork
		} else if !errors.Is(err, sessionstore.ErrSessionSnapshotConflict) {
			t.Fatalf("tick %d: save = %v, want a conflict", i, err)
		}
		info := conflictRecoveryTick(t, session, target)
		branches[info.Path] = true
		// The controller commits to the branch it just wrote.
		target = info.Path
	}

	if len(branches) > 1 {
		names := make([]string, 0, len(branches))
		for p := range branches {
			names = append(names, filepath.Base(p))
		}
		slices.Sort(names)
		t.Errorf("8 conflicts from one writer on one lineage produced %d recovery files, want 1:\n  %s",
			len(branches), strings.Join(names, "\n  "))
	}
}

func rewriteTranscriptOutsideReasonix(t *testing.T, path string, msgs ...provider.Message) {
	t.Helper()
	for _, p := range append([]string{path}, store.SessionSidecarFiles(path)...) {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			t.Fatalf("clear %s: %v", filepath.Base(p), err)
		}
	}
	outside := sessionstore.NewSession("sys")
	for _, m := range msgs {
		outside.Add(m)
	}
	if err := outside.Save(path); err != nil {
		t.Fatalf("outside write: %v", err)
	}
}

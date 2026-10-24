package isolation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/base/filelock"
	"reasonix/internal/base/fileutil"
	"reasonix/internal/contract/tool"
	"reasonix/internal/platform/gitcmd"
)

func TestChildReservationCrashHelper(t *testing.T) {
	root := os.Getenv("REASONIX_CHILD_RESERVATION_CRASH")
	if root == "" {
		return
	}
	repo := filepath.Join(root, "repo")
	s := NewStore(filepath.Join(root, "managed"), repo, opened(t, repo))
	parent, err := s.BeginOwned(t.Context(), repo, "owner")
	if err != nil {
		t.Fatal(err)
	}
	child := ChildResult{ID: "iso_98765432", Root: filepath.Join(root, "children"), Workspace: parent.Cand.WorkspaceRoot, Token: "reserved-before-snapshot"}
	if err := s.registerChild(t.Context(), parent, child); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "parent-id"), []byte(parent.ID), 0600); err != nil {
		t.Fatal(err)
	}
	os.Exit(77)
}

func TestParentSurvivesCrashBeforeChildRecord(t *testing.T) {
	repo := gitRepo(t)
	root := t.TempDir()
	cmd := gitcmd.Command(t.Context(), repo, "clone", "--quiet", repo, filepath.Join(root, "repo"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("clone: %v %s", err, out)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	cmd = exec.CommandContext(ctx, os.Args[0], "-test.run=^TestChildReservationCrashHelper$")
	cmd.Env = append(os.Environ(), "REASONIX_CHILD_RESERVATION_CRASH="+root)
	out, err := cmd.CombinedOutput()
	var exited *exec.ExitError
	if !errors.As(err, &exited) || exited.ExitCode() != 77 {
		t.Fatalf("helper: %v\n%s", err, out)
	}
	id, err := os.ReadFile(filepath.Join(root, "parent-id"))
	if err != nil {
		t.Fatal(err)
	}
	repo = filepath.Join(root, "repo")
	s := NewStore(filepath.Join(root, "managed"), repo, opened(t, repo))
	for name, operation := range map[string]func() error{
		"resume":  func() error { _, err := s.Resume(t.Context(), string(id), "owner"); return err },
		"discard": func() error { _, err := s.Discard(t.Context(), string(id)); return err },
		"apply":   func() error { _, err := s.Apply(t.Context(), string(id)); return err },
	} {
		if err := operation(); refusalCodeOf(err) != CodeDependentResults {
			t.Errorf("%s lost reservation: %v", name, err)
		}
	}
	records := s.durable()
	if len(records) != 1 {
		t.Fatalf("parent records: %d", len(records))
	}
	before := records[0]
	s.SweepExpired(t.Context(), -time.Second)
	if _, err := os.Stat(before.Cand.WorktreeRoot); err != nil {
		t.Fatal("sweep removed referenced parent", err)
	}
}

func TestDependencyFieldsSurviveOrdinarySaveAndSealing(t *testing.T) {
	repo := gitRepo(t)
	s := NewStore(t.TempDir(), repo, opened(t, repo))
	parent, err := s.BeginOwned(t.Context(), repo, "owner")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Abort(context.Background(), parent) })
	child := ChildResult{ID: "iso_12345678", Root: t.TempDir(), Workspace: parent.Cand.WorkspaceRoot, Token: "token"}
	if err := s.registerChild(t.Context(), parent, child); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(entryRecordPath(parent))
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(body, &fields); err != nil {
		t.Fatal(err)
	}
	lineage := fields["lineage"].(map[string]any)
	lineage["future_lineage"] = "retained"
	lineage["children"].(map[string]any)[child.ID].(map[string]any)["future_child"] = "retained"
	body, err = json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(entryRecordPath(parent), body, 0600); err != nil {
		t.Fatal(err)
	}
	if err := persist(parent); err != nil {
		t.Fatal(err)
	}
	if err := s.SealChildren(t.Context(), parent); refusalCodeOf(err) != CodeDependentResults {
		t.Fatal(err)
	}
	body, err = os.ReadFile(entryRecordPath(parent))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(body, &fields); err != nil {
		t.Fatal(err)
	}
	lineage = fields["lineage"].(map[string]any)
	if lineage["future_lineage"] != "retained" || lineage["children"].(map[string]any)[child.ID].(map[string]any)["future_child"] != "retained" {
		t.Fatalf("unknown dependency fields lost: %s", body)
	}
}

func TestTerminalChildMustFinishCleanupAndPhysicalRunBeforeUnpinning(t *testing.T) {
	repo := gitRepo(t)
	s := NewStore(t.TempDir(), repo, opened(t, repo))
	parent, err := s.BeginOwned(t.Context(), repo, "owner")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Abort(context.Background(), parent) })
	children := NewStore(t.TempDir(), parent.Cand.WorkspaceRoot, parent.Cand.Repo)
	child, err := s.BeginChild(t.Context(), parent, children, "child")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { children.Abort(context.Background(), child) })
	if _, err := children.Finish(t.Context(), child); err != nil {
		t.Fatal(err)
	}
	cleanupFailure := errors.New("injected cleanup finalization failure")
	children.cleanupProbe = func(*Entry) error { return cleanupFailure }
	if _, err := children.Discard(t.Context(), child.ID); !errors.Is(err, cleanupFailure) {
		t.Fatalf("cleanup fault: %v", err)
	}
	if err := s.SealChildren(t.Context(), parent); refusalCodeOf(err) != CodeDependentResults {
		t.Fatalf("cleanup not finished: %v", err)
	}
	children.cleanupProbe = nil
	if _, err := children.Discard(t.Context(), child.ID); err != nil {
		t.Fatal(err)
	}
	release, err := filelock.TryAcquire(children.runLock(child.ID))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SealChildren(t.Context(), parent); refusalCodeOf(err) != CodeDependentResults {
		release()
		t.Fatalf("live run released dependency: %v", err)
	}
	release()
	if err := s.SealChildren(t.Context(), parent); err != nil {
		t.Fatal(err)
	}
}

func TestChildRegistrationAndSealHaveOneWinner(t *testing.T) {
	for range 6 {
		repo := gitRepo(t)
		s := NewStore(t.TempDir(), repo, opened(t, repo))
		parent, err := s.BeginOwned(t.Context(), repo, "owner")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { s.Abort(context.Background(), parent) })
		start := make(chan struct{})
		registered, sealed := make(chan error, 1), make(chan error, 1)
		child := ChildResult{ID: "iso_12345678", Root: t.TempDir(), Workspace: parent.Cand.WorkspaceRoot, Token: "token"}
		go func() { <-start; registered <- s.registerChild(t.Context(), parent, child) }()
		go func() { <-start; sealed <- s.SealChildren(t.Context(), parent) }()
		close(start)
		r, f := <-registered, <-sealed
		if !((r == nil && refusalCodeOf(f) == CodeDependentResults) || (f == nil && refusalCodeOf(r) == CodeDependentResults)) {
			t.Fatalf("register=%v seal=%v", r, f)
		}
	}
}

func TestLineagePersistenceUsesStrictAtomicPublication(t *testing.T) {
	repo := gitRepo(t)
	s := NewStore(t.TempDir(), repo, opened(t, repo))
	parent, err := s.BeginOwned(t.Context(), repo, "owner")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Abort(context.Background(), parent) })
	if err := s.SealChildren(t.Context(), parent); err != nil {
		t.Fatal(err)
	}
	synced := false
	restore := fileutil.SetSyncParentDirForTest(func(path string) error {
		if path == entryRecordPath(parent) {
			synced = true
		}
		return nil
	})
	defer restore()
	if err := persist(parent); err != nil {
		t.Fatal(err)
	}
	if !synced {
		t.Fatal("v4 persist did not use strict atomic publication")
	}
}

func TestChildIdentityCollisionCannotReplaceSiblingDependency(t *testing.T) {
	repo := gitRepo(t)
	s := NewStore(t.TempDir(), repo, opened(t, repo))
	parent, err := s.BeginOwned(t.Context(), repo, "parent")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Abort(context.Background(), parent) })
	original := ChildResult{ID: "iso_12345678", Root: t.TempDir(), Workspace: parent.Cand.WorkspaceRoot, Token: "first"}
	if err := s.registerChild(t.Context(), parent, original); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(entryRecordPath(parent))
	if err != nil {
		t.Fatal(err)
	}
	collision := original
	collision.Root, collision.Token = filepath.Join(t.TempDir(), "other"), "second"
	if err := s.registerChild(t.Context(), parent, collision); err == nil {
		t.Fatal("duplicate child replaced original dependency")
	}
	after, err := os.ReadFile(entryRecordPath(parent))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("refused collision rewrote parent record")
	}
}

func TestChildAdmissionPinsParentBeforeSnapshot(t *testing.T) {
	repo := gitRepo(t)
	parentStore := NewStore(t.TempDir(), repo, opened(t, repo))
	parent, err := parentStore.BeginOwned(t.Context(), repo, "parent-session")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { parentStore.Abort(context.Background(), parent) })
	children := NewStore(t.TempDir(), parent.Cand.WorkspaceRoot, parent.Cand.Repo)
	child, err := parentStore.BeginChild(t.Context(), parent, children, "child-session")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { children.Abort(context.Background(), child) })
	var saved durableEntry
	body, err := os.ReadFile(entryRecordPath(parent))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(body, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Version != 4 || saved.Lineage == nil || len(saved.Lineage.Children) != 1 || child.Lineage == nil || child.Lineage.Parent == nil {
		t.Fatalf("parent dependency missing: parent=%+v child=%+v", saved.Lineage, child.Lineage)
	}
	if _, err := parentStore.Finish(t.Context(), parent); refusalCodeOf(err) != CodeDependentResults {
		t.Fatalf("parent sealed with live child: %v", err)
	}
	if _, err := os.Stat(parent.Cand.WorktreeRoot); err != nil {
		t.Fatal(err)
	}
	if _, err := children.Finish(t.Context(), child); err != nil {
		t.Fatal(err)
	}
	if _, err := parentStore.Discard(t.Context(), parent.ID); refusalCodeOf(err) != CodeDependentResults {
		t.Fatalf("discard removed pending child's parent: %v", err)
	}
	if _, err := parentStore.Resume(t.Context(), parent.ID, "parent-session"); refusalCodeOf(err) != CodeDependentResults {
		t.Fatalf("resume advanced generation with pending child: %v", err)
	}
	if _, err := children.Discard(t.Context(), child.ID); err != nil {
		t.Fatal(err)
	}
	parent, err = parentStore.Resume(t.Context(), parent.ID, "parent-session")
	if err != nil {
		t.Fatal(err)
	}
}

func TestParentPersistenceCannotLoseRegisteredChildren(t *testing.T) {
	repo := gitRepo(t)
	s := NewStore(t.TempDir(), repo, opened(t, repo))
	parent, err := s.BeginOwned(t.Context(), repo, "parent")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Abort(context.Background(), parent) })
	children := NewStore(t.TempDir(), parent.Cand.WorkspaceRoot, parent.Cand.Repo)
	child, err := s.BeginChild(t.Context(), parent, children, "child")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { children.Abort(context.Background(), child) })
	parent.Lineage = nil
	unlock, err := s.lockEntry(t.Context(), parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	err = persist(parent)
	unlock()
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(entryRecordPath(parent))
	if err != nil {
		t.Fatal(err)
	}
	var saved durableEntry
	if err := json.Unmarshal(body, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Version != 4 || saved.Lineage == nil || saved.Lineage.Children[child.ID].ID != child.ID {
		t.Fatalf("ordinary persist lost dependency or downgraded protocol: %s", body)
	}
}

func TestSealedParentRejectsNewChild(t *testing.T) {
	repo := gitRepo(t)
	s := NewStore(t.TempDir(), repo, opened(t, repo))
	parent, err := s.BeginOwned(t.Context(), repo, "parent")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Abort(context.Background(), parent) })
	children := NewStore(t.TempDir(), parent.Cand.WorkspaceRoot, parent.Cand.Repo)
	if err := s.SealChildren(t.Context(), parent); err != nil {
		t.Fatal(err)
	}
	if _, err := s.BeginChild(t.Context(), parent, children, "child"); refusalCodeOf(err) != CodeDependentResults {
		t.Fatalf("admission reopened after seal: %v", err)
	}
}

func TestStaleParentCannotRegisterChild(t *testing.T) {
	repo := gitRepo(t)
	s := NewStore(t.TempDir(), repo, opened(t, repo))
	parent, err := s.BeginOwned(t.Context(), repo, "parent")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Abort(context.Background(), parent) })
	stale := *parent
	stale.RunGeneration = "retired-generation"
	child := ChildResult{ID: "iso_12345678", Root: t.TempDir(), Workspace: parent.Cand.WorkspaceRoot, Token: "token"}
	if err := s.registerChild(t.Context(), &stale, child); refusalCodeOf(err) != CodeStaleExecution {
		t.Fatalf("stale registration: %v", err)
	}
}

func refusalCodeOf(err error) string {
	var refusal tool.Refusal
	if errors.As(err, &refusal) {
		return refusal.Code
	}
	return ""
}

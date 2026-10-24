package isolation

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/platform/gitcmd"
)

func TestGuardedChildPreparationCrashHelper(t *testing.T) {
	root := os.Getenv("REASONIX_GUARDED_CHILD_CRASH")
	if root == "" {
		return
	}
	repo := filepath.Join(root, "repo")
	s := NewStore(filepath.Join(root, "managed"), repo, opened(t, repo))
	parent, err := s.BeginOwned(t.Context(), repo, "owner")
	if err != nil {
		t.Fatal(err)
	}
	children := NewStore(filepath.Join(root, "children"), parent.Cand.WorkspaceRoot, parent.Cand.Repo)
	s.childRegistrationProbe = func(parent *Entry, _ ChildResult) {
		if err := os.WriteFile(filepath.Join(root, "parent-id"), []byte(parent.ID), 0600); err != nil {
			t.Fatal(err)
		}
		os.Exit(77)
	}
	_, err = s.BeginChild(t.Context(), parent, children, "child-owner")
	t.Fatalf("registration crash checkpoint not reached: %v", err)
}

func TestGuardedChildReservationRecoversAfterProcessDeath(t *testing.T) {
	repo, root := gitRepo(t), t.TempDir()
	cmd := gitcmd.Command(t.Context(), repo, "clone", "--quiet", repo, filepath.Join(root, "repo"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("clone: %v %s", err, out)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	cmd = exec.CommandContext(ctx, os.Args[0], "-test.run=^TestGuardedChildPreparationCrashHelper$")
	cmd.Env = append(os.Environ(), "REASONIX_GUARDED_CHILD_CRASH="+root)
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
	catalog, err := s.Catalog(t.Context())
	if err != nil || len(catalog.Results) != 1 || len(catalog.Results[0].Actions) != 1 {
		t.Fatalf("recoverable orphan has no host action: %+v %v", catalog, err)
	}
	parent, err := s.Resume(t.Context(), string(id), "owner")
	if err != nil {
		t.Fatalf("stopped preparer permanently pins parent: %v", err)
	}
	t.Cleanup(func() { s.Abort(context.Background(), parent) })
	if parent.Lineage == nil || len(parent.Lineage.Children) != 0 {
		t.Fatalf("reservation not reconciled: %+v", parent.Lineage)
	}
	if got := read(t, parent.Cand.WorkspaceRoot, "a.txt"); got != "one\n" {
		t.Fatal("recovery modified parent contents", got)
	}
}

func TestLiveLatePreparerCannotLoseItsReservation(t *testing.T) {
	repo := gitRepo(t)
	s := NewStore(t.TempDir(), repo, opened(t, repo))
	parent, err := s.BeginOwned(t.Context(), repo, "owner")
	if err != nil {
		t.Fatal(err)
	}
	children := NewStore(t.TempDir(), parent.Cand.WorkspaceRoot, parent.Cand.Repo)
	registered, proceed := make(chan struct{}), make(chan struct{})
	s.childRegistrationProbe = func(*Entry, ChildResult) { close(registered); <-proceed }
	type result struct {
		entry *Entry
		err   error
	}
	prepared := make(chan result, 1)
	go func() {
		child, err := s.BeginChild(t.Context(), parent, children, "child-owner")
		prepared <- result{child, err}
	}()
	<-registered
	s.endRun(parent)
	_, err = s.Resume(t.Context(), parent.ID, "owner")
	if refusalCodeOf(err) != CodeDependentResults {
		t.Errorf("late preparer was not retained: %v", err)
	}
	close(proceed)
	child := <-prepared
	if child.err != nil {
		t.Fatal(child.err)
	}
	if _, err := children.Finish(t.Context(), child.entry); err != nil {
		t.Fatal(err)
	}
	if _, err := children.Discard(t.Context(), child.entry.ID); err != nil {
		t.Fatal(err)
	}
	resumed, err := s.Resume(t.Context(), parent.ID, "owner")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Abort(context.Background(), resumed) })
}

func TestRecoveryPreviewWithSettledSiblingAndLiteralManagedRoot(t *testing.T) {
	repo := gitRepo(t)
	s := NewStore(t.TempDir(), repo, opened(t, repo))
	parent, err := s.BeginOwned(t.Context(), repo, "owner")
	if err != nil {
		t.Fatal(err)
	}
	children := NewStore(filepath.Join(t.TempDir(), "[children]"), parent.Cand.WorkspaceRoot, parent.Cand.Repo)
	child, err := s.BeginChild(t.Context(), parent, children, "child-owner")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := children.Finish(t.Context(), child); err != nil {
		t.Fatal(err)
	}
	if _, err := children.Discard(t.Context(), child.ID); err != nil {
		t.Fatal(err)
	}
	orphan := ChildResult{ID: "iso_98765432", Root: children.managedRoot, Workspace: children.workspace, Token: "stopped", Preparation: childPreparationV1}
	if err := s.registerChild(t.Context(), parent, orphan); err != nil {
		t.Fatal(err)
	}
	s.endRun(parent)
	before, err := os.ReadFile(entryPath(parent.Cand))
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := s.Catalog(t.Context())
	rows := map[string]ResultSummary{}
	for _, row := range catalog.Results {
		rows[row.ID] = row
	}
	if err != nil || len(rows[parent.ID].Actions) != 1 || !rows[child.ID].CleanupComplete || rows[child.ID].ParentID != parent.ID {
		t.Fatalf("settled sibling hides recoverable action or loses its parent identity: %+v %v", catalog, err)
	}
	after, err := os.ReadFile(entryPath(parent.Cand))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("catalog changed dependency record", err)
	}
	if _, err := s.PreviewDiscard(t.Context(), parent.ID); err != nil {
		t.Fatal(err)
	}
	d, err := s.managedResultMetadata(parent.ID)
	if err != nil || len(d.Lineage.Children) != 1 || d.Lineage.Children[child.ID].Token == "" {
		t.Fatalf("recovery dropped settled sibling identity: %+v %v", d.Lineage, err)
	}
	if got := read(t, repo, "a.txt"); got != "one\n" {
		t.Fatal("source changed", got)
	}
}

func TestUncertainChildEnumerationNeverRemovesReservation(t *testing.T) {
	for _, mode := range []string{"unidentified", "marker_directory", "unreadable_directory"} {
		t.Run(mode, func(t *testing.T) {
			repo := gitRepo(t)
			s := NewStore(t.TempDir(), repo, opened(t, repo))
			parent, err := s.BeginOwned(t.Context(), repo, "owner")
			if err != nil {
				t.Fatal(err)
			}
			root := t.TempDir()
			child := ChildResult{ID: "iso_98765432", Root: root, Workspace: parent.Cand.WorkspaceRoot, Token: "unknown", Preparation: childPreparationV1}
			if err := s.registerChild(t.Context(), parent, child); err != nil {
				t.Fatal(err)
			}
			s.endRun(parent)
			bad := filepath.Join(root, "unobserved")
			if err := os.Mkdir(bad, 0700); err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "unidentified":
				if err := os.WriteFile(filepath.Join(bad, entryFile), []byte("{"), 0600); err != nil {
					t.Fatal(err)
				}
			case "marker_directory":
				if err := os.Mkdir(filepath.Join(bad, entryFile), 0700); err != nil {
					t.Fatal(err)
				}
			case "unreadable_directory":
				if err := os.Chmod(bad, 0); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Chmod(bad, 0700) })
				if _, err := os.Lstat(filepath.Join(bad, entryFile)); !errors.Is(err, os.ErrPermission) {
					t.Skip("this user/filesystem does not enforce directory mode denial")
				}
			}
			before, err := os.ReadFile(entryPath(parent.Cand))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.operationSnapshot(t.Context(), parent.ID); err == nil {
				t.Fatal("incomplete enumeration was accepted as absence")
			}
			after, err := os.ReadFile(entryPath(parent.Cand))
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("uncertain child registration was removed", err)
			}
		})
	}
}

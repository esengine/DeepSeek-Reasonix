package isolation

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"reasonix/internal/base/fileutil"
	"reasonix/internal/platform/worktree"
	"reasonix/internal/state/mutation"
)

func TestRootReconcilesDescendantInterruptedPublicationThroughRegisteredSource(t *testing.T) {
	for _, mode := range []string{"allowed", "denied", "foreign_workspace"} {
		t.Run(mode, func(t *testing.T) {
			s, parent, children, child, i := nestedIntegrationFixture(t)
			s.WithMutationCoordinator(children.mutations)
			child.State, i.attempt.State = Applying, Applying
			if err := persist(child); err != nil {
				t.Fatal(err)
			}
			if err := i.save(); err != nil {
				t.Fatal(err)
			}
			id := i.ID()
			i.Close()
			preview, err := s.PreviewResultRecovery(t.Context(), child.ID)
			if err != nil || preview == nil || preview.ResultID != child.ID || !sameDir(preview.Workspace, parent.Cand.WorkspaceRoot) {
				t.Fatalf("descendant recovery did not bind its original source: %+v %v", preview, err)
			}
			before, err := os.ReadFile(entryRecordPath(child))
			if err != nil {
				t.Fatal(err)
			}
			var authorized []string
			denied := errors.New("current source policy denied recovery")
			authorize := func(_ context.Context, paths []string) error {
				authorized = append([]string{}, paths...)
				if mode == "denied" {
					return denied
				}
				return nil
			}
			workspace := preview.Workspace
			if mode == "foreign_workspace" {
				workspace = t.TempDir()
			}
			err = s.RecoverResultExpected(t.Context(), child.ID, workspace, preview.Revision, authorize)
			if mode != "allowed" {
				if err == nil {
					t.Fatal("descendant recovery bypassed current source authority")
				}
				after, readErr := os.ReadFile(entryRecordPath(child))
				if readErr != nil || !bytes.Equal(before, after) {
					t.Fatal("refused recovery changed its original manifest")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			want, err := fileutil.CanonicalWritePath(filepath.Join(s.workspace, "child.txt"))
			if err != nil || !slices.Equal(authorized, []string{want}) {
				t.Fatalf("recovery authorization did not project to the original workspace: %v %v", authorized, err)
			}
			original, err := children.managedResultMetadata(child.ID)
			if err != nil || original.State != Conflict {
				t.Fatalf("original recovery did not reconcile its state: %+v %v", original, err)
			}
			attempt, _, err := children.readIntegration(id)
			if err != nil || attempt.State != Conflict || attempt.Verification != "unknown" {
				t.Fatalf("attempt recovery invented publication or verification: %+v %v", attempt, err)
			}
			for _, root := range []string{s.workspace, parent.Cand.WorkspaceRoot} {
				if _, err := os.Stat(filepath.Join(root, "child.txt")); !os.IsNotExist(err) {
					t.Fatalf("journal-less recovery wrote source: %v", err)
				}
			}
		})
	}
}

func TestRootRecoversPhysicalDescendantJournalWithoutOverwritingLaterEdits(t *testing.T) {
	for _, externalEdit := range []bool{false, true} {
		t.Run(map[bool]string{false: "unchanged_postimage", true: "external_edit"}[externalEdit], func(t *testing.T) {
			s, parent, children, child := descendantFixture(t)
			coordinator, err := mutation.New(children.workspace, t.TempDir(), nil)
			if err != nil {
				t.Fatal(err)
			}
			s.WithMutationCoordinator(coordinator)
			children.WithMutationCoordinator(coordinator)
			write(t, child.Cand.WorkspaceRoot, "a.txt", "candidate a\n")
			write(t, child.Cand.WorkspaceRoot, "b.txt", "candidate b\n")
			if _, err := children.Finish(t.Context(), child); err != nil {
				t.Fatal(err)
			}
			i, err := children.PrepareIntegration(t.Context(), child.ID, "child")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(i.Close)
			checkPreparedIntegration(t, children, i)
			previous := fileutil.CrashPoint
			t.Cleanup(func() { fileutil.CrashPoint = previous })
			fileutil.CrashPoint = func(op, _ string) {
				first, firstErr := os.ReadFile(filepath.Join(parent.Cand.WorkspaceRoot, "a.txt"))
				_, secondErr := os.Stat(filepath.Join(parent.Cand.WorkspaceRoot, "b.txt"))
				if op == "atomic-write" && firstErr == nil && string(first) == "candidate a\n" && os.IsNotExist(secondErr) {
					panic("interrupted nested publication")
				}
			}
			crashed := false
			func() {
				defer func() { crashed = recover() != nil }()
				if err := i.Commit(t.Context(), func(context.Context, []string) error { return nil }); err != nil {
					t.Fatalf("commit failed before fault: %v", err)
				}
			}()
			fileutil.CrashPoint = previous
			i.Close()
			if got := read(t, parent.Cand.WorkspaceRoot, "a.txt"); !crashed || got != "candidate a\n" {
				t.Fatalf("nested publication did not reach partial application: crashed=%v a=%q changes=%+v", crashed, got, i.attempt.Changes)
			}
			if externalEdit {
				write(t, parent.Cand.WorkspaceRoot, "a.txt", "later user edit\n")
			}
			preview, err := s.PreviewResultRecovery(t.Context(), child.ID)
			if err != nil || preview == nil {
				t.Fatalf("root lost descendant's physical journal: %+v %v", preview, err)
			}
			allowed := map[string]bool{}
			for _, name := range []string{"a.txt", "b.txt"} {
				path, err := fileutil.CanonicalWritePath(filepath.Join(s.workspace, name))
				if err != nil {
					t.Fatal(err)
				}
				allowed[path] = true
			}
			authorized := map[string]bool{}
			err = s.RecoverResultExpected(t.Context(), child.ID, preview.Workspace, preview.Revision, func(_ context.Context, paths []string) error {
				for _, path := range paths {
					if !allowed[path] {
						t.Fatalf("physical recovery authorization escaped root projection: %s", path)
					}
					authorized[path] = true
				}
				return nil
			})
			if len(authorized) != 2 {
				t.Fatal("physical recovery bypassed root path authorization")
			}
			if externalEdit {
				if err == nil || read(t, parent.Cand.WorkspaceRoot, "a.txt") != "later user edit\n" {
					t.Fatalf("recovery overwrote an external edit: %v", err)
				}
			} else {
				if err != nil || read(t, parent.Cand.WorkspaceRoot, "a.txt") != "one\n" {
					t.Fatalf("physical recovery failed: %v", err)
				}
				if err := worktree.CheckApplyRecovery(parent.Cand.Repo); err != nil {
					t.Fatalf("physical journal remained after recovery: %v", err)
				}
			}
			if read(t, s.workspace, "a.txt") != "one\n" {
				t.Fatal("descendant recovery wrote the root workspace")
			}
		})
	}
}

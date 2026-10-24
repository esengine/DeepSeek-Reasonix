package isolation

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/fileutil"
	"reasonix/internal/state/mutation"
)

func nestedIntegrationFixture(t *testing.T) (*Store, *Entry, *Store, *Entry, *Integration) {
	t.Helper()
	s, parent, children, child := descendantFixture(t)
	coordinator, err := mutation.New(children.workspace, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	children.WithMutationCoordinator(coordinator)
	write(t, child.Cand.WorkspaceRoot, "child.txt", "retained child\n")
	if _, err := children.Finish(t.Context(), child); err != nil {
		t.Fatal(err)
	}
	i, err := children.PrepareIntegration(t.Context(), child.ID, "child")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(i.Close)
	return s, parent, children, child, i
}

func TestChildIntegrationPinsDirectParentUntilCleanupAndOwnerClose(t *testing.T) {
	s, parent, children, child, i := nestedIntegrationFixture(t)
	if _, err := children.Discard(t.Context(), child.ID); err != nil {
		t.Fatal(err)
	}
	s.endRun(parent)
	for _, op := range []func() error{
		func() error { _, err := s.Resume(t.Context(), parent.ID, "parent"); return err },
		func() error { _, err := s.Discard(t.Context(), parent.ID); return err },
	} {
		if err := op(); refusalCodeOf(err) != CodeDependentResults {
			t.Fatalf("pending integration did not retain parent: %v", err)
		}
	}
	if err := i.Discard(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Resume(t.Context(), parent.ID, "parent"); refusalCodeOf(err) != CodeDependentResults {
		t.Fatalf("live integration owner did not retain parent: %v", err)
	}
	i.Close()
	resumed, err := s.Resume(t.Context(), parent.ID, "parent")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Abort(context.Background(), resumed) })
}

func TestChildCannotResumeAcrossPendingIntegrationOrDowngradeProtocol(t *testing.T) {
	_, _, children, child, i := nestedIntegrationFixture(t)
	i.Close()
	if _, err := children.Resume(t.Context(), child.ID, "child"); refusalCodeOf(err) != CodeDependentResults {
		t.Fatalf("child advanced generation across pending integration: %v", err)
	}
	if err := persist(child); err != nil {
		t.Fatal(err)
	}
	if err := children.SealChildren(t.Context(), child); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(entryRecordPath(child))
	if err != nil {
		t.Fatal(err)
	}
	var d durableEntry
	if err := json.Unmarshal(raw, &d); err != nil {
		t.Fatal(err)
	}
	if d.Version != 5 {
		t.Fatalf("nested integration dependency lost its aware-reader boundary: version %d", d.Version)
	}
}

func TestIntegrationRegistrationCannotPublishAfterChildAndParentDiscard(t *testing.T) {
	s, parent, children, child := descendantFixture(t)
	coordinator, err := mutation.New(children.workspace, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	children.WithMutationCoordinator(coordinator)
	if _, err := children.Finish(t.Context(), child); err != nil {
		t.Fatal(err)
	}
	ready, proceed := make(chan struct{}), make(chan struct{})
	children.integrationRegistrationProbe = func(*Entry) { close(ready); <-proceed }
	finished := make(chan error, 1)
	go func() {
		i, err := children.PrepareIntegration(t.Context(), child.ID, "child")
		if i != nil {
			i.Close()
		}
		finished <- err
	}()
	<-ready
	if _, err := children.Discard(t.Context(), child.ID); err != nil {
		close(proceed)
		<-finished
		t.Fatal(err)
	}
	s.endRun(parent)
	if _, err := s.Discard(t.Context(), parent.ID); err != nil {
		close(proceed)
		<-finished
		t.Fatal(err)
	}
	close(proceed)
	if err := <-finished; err == nil {
		t.Fatal("late integration registration succeeded after source removal")
	}
	paths, err := children.integrationRecordPaths()
	if err != nil || len(paths) != 0 {
		t.Fatalf("failed registration or its deferred recovery published an orphan: %v %v", paths, err)
	}
}

func TestRootCanManageRetainedDescendantIntegration(t *testing.T) {
	s, parent, children, child, i := nestedIntegrationFixture(t)
	s.WithMutationCoordinator(children.mutations)
	id := i.ID()
	i.Close()
	if _, err := children.Discard(t.Context(), child.ID); err != nil {
		t.Fatal(err)
	}
	catalog, err := s.Catalog(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, row := range catalog.Results {
		if row.ID == id {
			found = true
			if row.ResultID != child.ID || row.ParentID != parent.ID || !sameDir(row.TargetWorkspace, parent.Cand.WorkspaceRoot) {
				t.Fatalf("integration target identity lost: %+v", row)
			}
		}
	}
	if !found {
		t.Fatal("root cannot discover the integration pinning its direct environment")
	}
	preview, err := s.PreviewDiscard(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DiscardResultExpected(t.Context(), id, preview.Revision); err != nil {
		t.Fatal(err)
	}
	if err := s.SealChildren(t.Context(), parent); err != nil {
		t.Fatalf("managed integration still pins source after explicit cleanup: %v", err)
	}
}

func TestIntegrationRegistrationReadsOnlyTheLockedManifest(t *testing.T) {
	s, entry := integrationFixture(t)
	parked := entry.Cand.WorktreeRoot + ".parked"
	s.integrationRegistrationProbe = func(*Entry) {
		if err := os.Rename(entry.Cand.WorktreeRoot, parked); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { _ = os.Rename(parked, entry.Cand.WorktreeRoot) })
	i, err := s.PrepareIntegration(t.Context(), entry.ID, "owner")
	if err != nil {
		t.Fatalf("metadata registration accessed the unlocked candidate: %v", err)
	}
	i.Close()
}

func TestNestedIntegrationVersionBarrierSurvivesFirstRecordFailure(t *testing.T) {
	s, parent, children, child := descendantFixture(t)
	coordinator, err := mutation.New(children.workspace, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	children.WithMutationCoordinator(coordinator)
	if _, err := children.Finish(t.Context(), child); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("injected interruption after version barrier publication")
	restore := fileutil.SetSyncParentDirForTest(func(path string) error {
		if path == entryRecordPath(child) {
			panic(failure)
		}
		return nil
	})
	_, prepareErr := children.PrepareIntegration(t.Context(), child.ID, "child")
	restore()
	if prepareErr == nil {
		t.Fatalf("missing injected publication failure: %v", prepareErr)
	}
	d, _, err := readLineageRecord(entryRecordPath(child))
	if err != nil || d.Version != 5 {
		t.Fatalf("version barrier was lost or unreadable: %d %v", d.Version, err)
	}
	paths, err := children.integrationRecordPaths()
	if err != nil || len(paths) != 0 {
		t.Fatalf("failed registration created a deferred orphan: %v %v", paths, err)
	}
	if _, err := children.Discard(t.Context(), child.ID); err != nil {
		t.Fatalf("durable barrier without an attempt became unrecoverable: %v", err)
	}
	if err := s.SealChildren(t.Context(), parent); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(parent.Cand.WorkspaceRoot, "a.txt")); err != nil {
		t.Fatalf("child recovery destroyed parent content: %v", err)
	}
}

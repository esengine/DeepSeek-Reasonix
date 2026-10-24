package isolation

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"reasonix/internal/base/filelock"
	"reasonix/internal/platform/worktree"
	"reasonix/internal/state/mutation"
)

func importedFixture(t *testing.T) (*Store, SnapshotImport, *Entry) {
	t.Helper()
	root := gitRepo(t)
	baseline, err := worktree.TakeSnapshot(t.Context(), opened(t, root))
	if err != nil {
		t.Fatal(err)
	}
	live, err := worktree.CreateCandidate(t.Context(), baseline, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = worktree.RemoveCandidate(t.Context(), baseline, live) })
	write(t, live.WorkspaceRoot, "a.txt", "exported\n")
	frozen, err := worktree.TakeSnapshot(t.Context(), live.Repo)
	if err != nil {
		t.Fatal(err)
	}
	coordinator, err := mutation.New(root, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	s := NewStore(t.TempDir(), root, opened(t, root)).WithMutationCoordinator(coordinator)
	request := SnapshotImport{ID: "iso_1234abcd", Owner: "root-owner", Baseline: baseline,
		Origin: SnapshotOrigin{Version: 1, PublicationID: "publication", EnvironmentID: "environment", EnvironmentGeneration: "generation", Materialization: frozen}}
	e, err := s.ImportSnapshotResult(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	return s, request, e
}

func TestImportedChecksReleaseResourcesAndExcludeApplyDiscardAndAnotherCheck(t *testing.T) {
	s, request, e := importedFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	entered, done := make(chan struct{}), make(chan error, 1)
	go func() {
		_, err := s.RecheckImportedResult(ctx, request, func(ctx context.Context, _ *Entry) ([]CheckEvidence, error) {
			close(entered)
			<-ctx.Done()
			return nil, ctx.Err()
		})
		done <- err
	}()
	<-entered
	l, err := s.mutations.Acquire(t.Context(), mutation.Intent{Effect: mutation.NamedPaths, Paths: []string{s.workspace}})
	if err != nil {
		t.Fatal(err)
	}
	l.End(mutation.NotExecuted)
	if _, err := s.Apply(t.Context(), e.ID); err == nil {
		t.Fatal("apply entered a running check")
	}
	if _, err := s.PreviewDiscard(t.Context(), e.ID); err == nil {
		t.Fatal("discard inspected a running check")
	}
	if _, err := s.RecheckImportedResult(t.Context(), request, func(context.Context, *Entry) ([]CheckEvidence, error) {
		t.Error("second check executed")
		return nil, nil
	}); err == nil {
		t.Fatal("second check acquired execution")
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	after, err := s.operationSnapshot(t.Context(), e.ID)
	if err != nil || after.State != Pending || after.Tree != e.Tree || !reflect.DeepEqual(after.Changes, e.Changes) {
		t.Fatalf("cancel lost sealed result: %+v %v", after, err)
	}
}

func TestImportedChecksTamperingPreservesManifestAndRequiresRecovery(t *testing.T) {
	s, request, before := importedFixture(t)
	after, err := s.RecheckImportedResult(t.Context(), request, func(_ context.Context, e *Entry) ([]CheckEvidence, error) {
		write(t, e.Cand.WorkspaceRoot, "a.txt", "tampered\n")
		return nil, nil
	})
	if err == nil || after.State != RecoveryRequired || after.Tree != before.Tree || !reflect.DeepEqual(after.Changes, before.Changes) {
		t.Fatalf("tampering accepted: %+v %v", after, err)
	}
}

func TestImportedChecksCrashProcess(t *testing.T) {
	raw := os.Getenv("REASONIX_IMPORT_CHECK_CRASH")
	if raw == "" {
		return
	}
	var input struct {
		Root, Managed string
		Request       SnapshotImport
	}
	if err := json.Unmarshal([]byte(raw), &input); err != nil {
		t.Fatal(err)
	}
	coordinator, err := mutation.New(input.Root, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	s := NewStore(input.Managed, input.Root, opened(t, input.Root)).WithMutationCoordinator(coordinator)
	_, err = s.RecheckImportedResult(t.Context(), input.Request, func(context.Context, *Entry) ([]CheckEvidence, error) { os.Exit(23); return nil, nil })
	t.Fatalf("crash boundary not reached: %v", err)
}

func TestImportedChecksRecoverSameResultAfterProcessDeath(t *testing.T) {
	s, request, before := importedFixture(t)
	raw, err := json.Marshal(struct {
		Root, Managed string
		Request       SnapshotImport
	}{s.workspace, s.managedRoot, request})
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestImportedChecksCrashProcess$")
	cmd.Env = append(os.Environ(), "REASONIX_IMPORT_CHECK_CRASH="+string(raw))
	out, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 23 {
		t.Fatalf("child: %v %s", err, out)
	}
	if _, err := s.ImportSnapshotResult(t.Context(), request); err != nil {
		t.Fatal("restart cannot discover immutable result", err)
	}
	after, err := s.RecheckImportedResult(t.Context(), request, func(context.Context, *Entry) ([]CheckEvidence, error) { return nil, nil })
	if err != nil || after.State != Pending || after.RunGeneration != before.RunGeneration || after.Tree != before.Tree || !reflect.DeepEqual(after.Changes, before.Changes) {
		t.Fatalf("restart lost immutable manifest: %+v %v", after, err)
	}
	if _, err := os.Stat(filepath.Join(after.Cand.WorktreeRoot, "a.txt")); err != nil {
		t.Fatal(err)
	}
	release, err := filelock.TryAcquire(s.runLock(after.ID))
	if err != nil {
		t.Fatal(err)
	}
	release()
}

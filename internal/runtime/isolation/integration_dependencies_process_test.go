package isolation

import (
	"errors"
	"os"
	"os/exec"
	"testing"

	"reasonix/internal/base/fileutil"
	"reasonix/internal/state/mutation"
)

func TestNestedIntegrationRegistrationCrashHelper(t *testing.T) {
	root := os.Getenv("REASONIX_NESTED_INTEGRATION_CRASH_ROOT")
	if root == "" {
		return
	}
	workspace, id := os.Getenv("REASONIX_NESTED_INTEGRATION_CRASH_WORKSPACE"), os.Getenv("REASONIX_NESTED_INTEGRATION_CRASH_ID")
	s := NewStore(root, workspace, opened(t, workspace))
	coordinator, err := mutation.New(workspace, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	s.WithMutationCoordinator(coordinator)
	d, err := s.managedResultMetadata(id)
	if err != nil {
		t.Fatal(err)
	}
	restore := fileutil.SetSyncParentDirForTest(func(path string) error {
		if path == entryRecordPath(d.entry()) {
			os.Exit(77)
		}
		return nil
	})
	defer restore()
	_, err = s.PrepareIntegration(t.Context(), id, "child")
	t.Fatalf("crash boundary was not reached: %v", err)
}

func TestRootRecoversChildAfterCrashBetweenVersionBarrierAndIntegrationRecord(t *testing.T) {
	s, parent, children, child := descendantFixture(t)
	if _, err := children.Finish(t.Context(), child); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestNestedIntegrationRegistrationCrashHelper$")
	cmd.Env = append(os.Environ(), "REASONIX_NESTED_INTEGRATION_CRASH_ROOT="+children.managedRoot,
		"REASONIX_NESTED_INTEGRATION_CRASH_WORKSPACE="+children.workspace, "REASONIX_NESTED_INTEGRATION_CRASH_ID="+child.ID)
	out, err := cmd.CombinedOutput()
	var exited *exec.ExitError
	if !errors.As(err, &exited) || exited.ExitCode() != 77 {
		t.Fatalf("helper did not crash after durable barrier: %v %s", err, out)
	}
	d, _, err := readLineageRecord(entryRecordPath(child))
	if err != nil || d.Version != 5 {
		t.Fatalf("crashed child barrier unreadable: %d %v", d.Version, err)
	}
	paths, err := children.integrationRecordPaths()
	if err != nil || len(paths) != 0 {
		t.Fatalf("unexpected late integration record: %v %v", paths, err)
	}
	preview, err := s.PreviewDiscard(t.Context(), child.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DiscardResultExpected(t.Context(), child.ID, preview.Revision); err != nil {
		t.Fatal(err)
	}
	if err := s.SealChildren(t.Context(), parent); err != nil {
		t.Fatalf("dead preparation owner retained a permanent pin: %v", err)
	}
}

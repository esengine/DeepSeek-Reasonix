package isolation

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"testing"
	"time"

	"reasonix/internal/state/mutation"
)

func TestPreviousV4CannotRewriteChildOrRemoveSourceAfterIntegrationRegistration(t *testing.T) {
	binary := os.Getenv("REASONIX_PREVIOUS_NESTED_READER")
	if binary == "" {
		t.Skip("independently built previous v4 owner binary not supplied")
	}
	s, parent, children, child := descendantFixture(t)
	coordinator, err := mutation.New(children.workspace, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	children.WithMutationCoordinator(coordinator)
	if _, err := children.Finish(t.Context(), child); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	cmd := previousNestedOwner(ctx, binary, children, child.ID, "stale-save")
	input, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); _ = cmd.Wait() })
	reader := bufio.NewReader(output)
	line, err := reader.ReadBytes('\n')
	if err != nil {
		t.Fatal(err)
	}
	var before durableEntry
	if err := json.Unmarshal(line, &before); err != nil || before.Version != 4 || before.ID != child.ID || before.Lineage.Parent == nil {
		t.Fatalf("previous owner did not capture the real nested v4 result: %s %v", line, err)
	}
	i, err := children.PrepareIntegration(ctx, child.ID, "child")
	if err != nil {
		t.Fatal(err)
	}
	defer i.Close()
	raw, err := os.ReadFile(entryRecordPath(child))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := input.Write([]byte{1}); err != nil {
		t.Fatal(err)
	}
	_ = input.Close()
	assertPreviousNestedRefusal(t, reader)
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(entryRecordPath(child))
	if err != nil || !bytes.Equal(raw, after) {
		t.Fatalf("old owner rewrote the v5 dependency boundary: %v", err)
	}
	if _, err := children.Discard(ctx, child.ID); err != nil {
		t.Fatal(err)
	}
	s.endRun(parent)
	parentRaw, err := os.ReadFile(entryRecordPath(parent))
	if err != nil {
		t.Fatal(err)
	}
	parentCmd := previousNestedOwner(ctx, binary, s, parent.ID, "discard-parent")
	out, err := parentCmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	assertPreviousNestedRefusal(t, bytes.NewReader(out))
	after, err = os.ReadFile(entryRecordPath(parent))
	if err != nil || !bytes.Equal(parentRaw, after) {
		t.Fatalf("old parent owner changed a pinned source record: %v", err)
	}
}

func previousNestedOwner(ctx context.Context, binary string, store *Store, id, action string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, binary, "-test.run=^TestPreviousNestedDependencyOwner$")
	cmd.Env = append(os.Environ(), "REASONIX_PREVIOUS_NESTED_REPO="+store.workspace,
		"REASONIX_PREVIOUS_NESTED_ROOT="+store.managedRoot, "REASONIX_PREVIOUS_NESTED_ID="+id,
		"REASONIX_PREVIOUS_NESTED_ACTION="+action)
	cmd.Stderr = os.Stderr
	return cmd
}

func assertPreviousNestedRefusal(t *testing.T, reader io.Reader) {
	t.Helper()
	var response struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(reader).Decode(&response); err != nil || response.Code != CodeUnsupportedState {
		t.Fatalf("previous v4 owner ignored aware-reader boundary: %+v %v", response, err)
	}
}

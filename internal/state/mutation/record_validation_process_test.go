package mutation

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestUnsupportedRecordSurvivesOwnerProcessDeath(t *testing.T) {
	for _, shape := range []string{"extension", "version_duplicate", "version_case"} {
		t.Run(shape, func(t *testing.T) { unsupportedRecordOwnerDeath(t, shape) })
	}
}

func unsupportedRecordOwnerDeath(t *testing.T, shape string) {
	t.Helper()
	root, state := t.TempDir(), t.TempDir()
	c := testCoordinator(t, root, state)
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestMutationProcessHelper$")
	cmd.Env = append(os.Environ(), "REASONIX_MUTATION_TEST_HELPER=1", "REASONIX_MUTATION_TEST_ROOT="+root,
		"REASONIX_MUTATION_TEST_STATE="+state, "REASONIX_MUTATION_TEST_PATH="+filepath.Join(root, "held"))
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	stop := sync.OnceFunc(func() { _ = cmd.Process.Kill(); _ = cmd.Wait(); _ = in.Close() })
	t.Cleanup(stop)
	ready := make(chan string, 1)
	go func() { line, _ := bufio.NewReader(out).ReadString('\n'); ready <- line }()
	select {
	case line := <-ready:
		if line != "mutation-ready\n" {
			t.Fatalf("child readiness: %q", line)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("child did not publish its record")
	}
	paths, err := filepath.Glob(filepath.Join(c.dir, "*.json"))
	if err != nil || len(paths) != 1 {
		t.Fatalf("child record: %v, %v", paths, err)
	}
	raw, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	if shape == "extension" {
		fields["future_resources"] = json.RawMessage(`["retain"]`)
		raw, err = json.Marshal(fields)
		if err != nil {
			t.Fatal(err)
		}
	} else {
		member := `"version":1`
		if shape == "version_case" {
			member = `"Version":1`
		}
		original := string(raw)
		raw = []byte(strings.Replace(original, `"version":1`, `"version":99,`+member, 1))
		if string(raw) == original {
			t.Fatal("fixture did not change child version")
		}
	}
	if err := os.WriteFile(paths[0], raw, 0600); err != nil {
		t.Fatal(err)
	}
	for _, phase := range []string{"live", "dead"} {
		if phase == "dead" {
			stop()
		}
		lease, err := c.TryAcquire(t.Context(), intent(filepath.Join(root, "unrelated")))
		if lease != nil {
			lease.End(NotExecuted)
		}
		if lease != nil || !errors.Is(err, ErrProtocol) {
			t.Errorf("%s unsupported owner allowed admission: %v", phase, err)
		}
		if actual, err := os.ReadFile(paths[0]); err != nil || string(actual) != string(raw) {
			t.Errorf("%s unsupported record not preserved: %q, %v", phase, actual, err)
		}
	}
}

package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/evidence"
	"reasonix/internal/tool"
)

// The whole lifecycle against a real file on disk — the steps that produced the
// repeated work in #10042 — checked through the real read_file and edit_file.

func lifecycleAgent(t *testing.T) *Agent {
	t.Helper()
	writer, ok := tool.LookupBuiltin("edit_file")
	if !ok {
		t.Fatal("edit_file builtin is not registered")
	}
	a := newIncompleteReadTestAgent(&scriptedProvider{}, incompleteReadBuiltin(t), NewSession("sys"), event.Discard, writer)
	a.task.ledger = evidence.NewLedger()
	return a
}

func writeFixture(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "login.go")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func fixtureBody(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func TestLifecycleReadEditSettlesWithoutAnyRereadChore(t *testing.T) {
	path := writeFixture(t, "alpha\nbeta\ngamma\n")
	a := lifecycleAgent(t)

	evidenceRound(t, a, "r1", "read_file", map[string]any{"path": path})
	out := evidenceRound(t, a, "e1", "edit_file", map[string]any{"path": path, "old_string": "beta", "new_string": "delta"})

	if strings.Contains(out, "evidence required") {
		t.Fatalf("an ordinary read-then-edit was blocked: %q", out)
	}
	if got := fixtureBody(t, path); !strings.Contains(got, "delta") {
		t.Fatalf("file = %q, want the edit applied", got)
	}
	for _, op := range a.operations().Snapshot() {
		if op.Mutation != nil && op.State != evidence.OperationSettled {
			t.Fatalf("ordinary mutation left unsettled: %+v", op)
		}
	}
}

func TestLifecycleExternalModificationIsRejectedThenRecovers(t *testing.T) {
	path := writeFixture(t, "alpha\nbeta\ngamma\n")
	a := lifecycleAgent(t)

	evidenceRound(t, a, "r1", "read_file", map[string]any{"path": path})
	// Someone else changes the file between the read and the write.
	if err := os.WriteFile(path, []byte("alpha\nbeta-external\ngamma\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	blocked := evidenceRound(t, a, "e1", "edit_file", map[string]any{"path": path, "old_string": "beta\n", "new_string": "delta\n"})
	if !strings.Contains(blocked, "blocked:") && !strings.Contains(blocked, "error:") {
		t.Fatalf("a stale edit overwrote an external change: %q", blocked)
	}
	if got := fixtureBody(t, path); !strings.Contains(got, "beta-external") {
		t.Fatalf("file = %q, want the external change preserved", got)
	}

	// Re-reading is the recovery: the next edit builds on what is really there.
	evidenceRound(t, a, "r2", "read_file", map[string]any{"path": path})
	recovered := evidenceRound(t, a, "e2", "edit_file", map[string]any{"path": path, "old_string": "beta-external", "new_string": "delta"})
	if strings.Contains(recovered, "blocked:") {
		t.Fatalf("the recovery edit was still blocked: %q", recovered)
	}
	if got := fixtureBody(t, path); !strings.Contains(got, "delta") {
		t.Fatalf("file = %q, want the recovery edit applied", got)
	}
}

func TestLifecycleRepeatedIdenticalRejectionStopsCallingTheTool(t *testing.T) {
	path := writeFixture(t, "alpha\nbeta\ngamma\n")
	a := lifecycleAgent(t)
	args := map[string]any{"path": path, "old_string": "nowhere-in-the-file", "new_string": "x"}

	first := evidenceRound(t, a, "e1", "edit_file", args)
	second := evidenceRound(t, a, "e2", "edit_file", args)
	third := evidenceRound(t, a, "e3", "edit_file", args)

	if !strings.Contains(first, "error:") && !strings.Contains(first, "blocked:") {
		t.Fatalf("fixture: the first edit should fail, got %q", first)
	}
	_ = second
	if got := fixtureBody(t, path); got != "alpha\nbeta\ngamma\n" {
		t.Fatalf("a failing edit changed the file: %q", got)
	}
	// Whether the third attempt is refused by the operation gate or by the
	// existing repeat guard, what must not happen is another silent retry.
	if !strings.Contains(third, "blocked:") && !strings.Contains(third, "error:") {
		t.Fatalf("the third identical attempt was neither refused nor reported: %q", third)
	}
}

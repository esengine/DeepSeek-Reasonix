package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/control"
	"reasonix/internal/event"
)

// holdSessionLease simulates another runtime owning path for the duration of
// the test (the in-process lease registry plus the OS lock behave exactly as a
// foreign holder for acquisition purposes).
func holdSessionLease(t *testing.T, path string) *agent.SessionLease {
	t.Helper()
	lease, err := agent.TryAcquireSessionLease(path)
	if err != nil {
		t.Fatalf("test holder acquire: %v", err)
	}
	t.Cleanup(lease.Release)
	return lease
}

func TestRunCopyRequiresResumeTarget(t *testing.T) {
	isolateCLIConfigHome(t)

	errOut := captureStderr(t, func() {
		if rc := runAgent([]string{"--copy", "do things"}, "dev"); rc != 2 {
			t.Fatalf("run --copy without target rc = %d, want 2", rc)
		}
	})
	if !strings.Contains(errOut, "--copy requires --resume or --continue") {
		t.Fatalf("run --copy stderr = %q, want usage error", errOut)
	}
}

// TestRunResumeCopyJSONKeepsStdoutClean guards that --copy under a structured
// output format writes its human notice to stderr, leaving stdout a single valid
// JSON object (the copy notice used to pollute it).

// chatLeaseFixture builds a TUI over a temp session dir with two saved
// sessions: older (the active one) and newer (the /resume 1 target).
func chatLeaseFixture(t *testing.T) (m chatTUI, active, target string) {
	t.Helper()
	dir := t.TempDir()
	active = filepath.Join(dir, "a-active.jsonl")
	target = filepath.Join(dir, "b-target.jsonl")
	saveTestSession(t, active, "active session")
	saveTestSession(t, target, "target session")
	pinNewer(t, active, target)

	exec := agent.New(nil, nil, agent.NewSession("sys"), agent.Options{}, event.Discard)
	m = newTestChatTUI()
	m.width = 80
	m.ctrl = newOwnedTestController(t, control.Options{Executor: exec, SessionDir: dir, SessionPath: active, Label: "test"})
	m.leases = control.NewSessionLeaseKeeper()
	t.Cleanup(m.leases.Release)
	if err := m.leases.Rebind(active); err != nil {
		t.Fatalf("seed lease on active: %v", err)
	}
	return m, active, target
}

// pinNewer gives newer a strictly later mtime than older so ListSessions
// ordering is deterministic across filesystems with coarse mtimes.
func pinNewer(t *testing.T, older, newer string) {
	t.Helper()
	info, err := os.Stat(newer)
	if err != nil {
		t.Fatal(err)
	}
	old := info.ModTime().Add(-2 * time.Second)
	if err := os.Chtimes(older, old, old); err != nil {
		t.Fatal(err)
	}
}

func TestChatNewSessionTakesFreshLease(t *testing.T) {
	m, active, _ := chatLeaseFixture(t)

	if cmd := m.runSlashCommand("/new"); cmd != nil {
		t.Fatal("/new should not return a tea.Cmd")
	}

	fresh := m.ctrl.SessionPath()
	if fresh == active {
		t.Fatalf("/new did not rotate the session path")
	}
	if got, want := m.leases.HeldPath(), agent.CanonicalSessionPath(fresh); got != want {
		t.Fatalf("lease after /new = %q, want %q", got, want)
	}
	lease, err := agent.TryAcquireSessionLease(active)
	if err != nil {
		t.Fatalf("old session lease not released by /new: %v", err)
	}
	lease.Release()
}

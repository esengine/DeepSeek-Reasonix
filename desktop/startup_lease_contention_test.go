package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/config"
)

// TestEnsureTabSessionLeaseForRebuildSurvivesTransientHolder reproduces the
// startup "this session is already open in another Reasonix window" false
// positive: a transient lease holder — CleanupStaleRunning probing a running
// subagent's parent session during a concurrent controller build — holds the
// session lease for a few milliseconds while the tab's own startup bind runs.
// The bind must retry against the genuinely-free lease instead of surfacing a
// spurious ErrSessionLeaseHeld.
func TestEnsureTabSessionLeaseForRebuildSurvivesTransientHolder(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	path := filepath.Join(dir, "contended-session.jsonl")

	tab := &WorkspaceTab{ID: "tab", Scope: "global", Ready: true, SessionPath: path}
	app := &App{
		tabs:     map[string]*WorkspaceTab{tab.ID: tab},
		tabOrder: []string{tab.ID},
	}
	t.Cleanup(tab.releaseSessionLease)

	// Simulate CleanupStaleRunning's transient parent-session lease probe:
	// acquire, hold briefly, then release. The probe targets the same runtime
	// key the tab's bind uses (case-folded on Windows), so the contention is
	// real on every platform.
	key := sessionRuntimeKey(path)
	acquired := make(chan struct{})
	releaseProbe := make(chan struct{})
	probeDone := make(chan struct{})
	go func() {
		defer close(probeDone)
		lease, err := agent.TryAcquireSessionLease(key)
		if err != nil {
			t.Errorf("probe lease acquire: %v", err)
			close(acquired)
			return
		}
		close(acquired)
		<-releaseProbe
		lease.Release()
	}()

	<-acquired // the probe now holds the lease

	bindErr := make(chan error, 1)
	go func() {
		bindErr <- app.ensureTabSessionLeaseForRebuild(tab, path, "")
	}()

	// Give the first bind attempt time to fail against the held lease, then
	// release the probe: the bind must succeed on a later attempt.
	time.Sleep(50 * time.Millisecond)
	close(releaseProbe)

	select {
	case err := <-bindErr:
		if err != nil {
			t.Fatalf("startup bind failed against a transient holder: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("startup bind did not complete after the transient holder released")
	}
	<-probeDone

	if key := tab.sessionLeaseRuntimeKey(); key != sessionRuntimeKey(path) {
		t.Fatalf("tab lease key = %q, want %q", key, sessionRuntimeKey(path))
	}
}

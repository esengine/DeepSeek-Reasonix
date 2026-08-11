package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/config"
)

// TestEnsureTabSessionLeaseForRebuildWaitsForMaintenanceHolder reproduces the
// startup "this session is already open in another Reasonix window" false
// positive: a transient lease holder — CleanupStaleRunning probing a running
// subagent's parent session during a concurrent controller build — holds the
// session lease for a few milliseconds while the tab's own startup bind runs.
// The bind must retry against the genuinely-free lease instead of surfacing a
// spurious ErrSessionLeaseHeld.
func TestEnsureTabSessionLeaseForRebuildWaitsForMaintenanceHolder(t *testing.T) {
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
		lease, err := agent.TryAcquireSessionMaintenanceLease(key)
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

	// Keep the maintenance owner past the old 2x50ms sleep-based retry window.
	// Startup must wait for this exact generation's release signal, not time out
	// and misreport another Reasonix window.
	time.Sleep(200 * time.Millisecond)
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

// TestCurrentProcessRuntimeHolderRemainsARealConflict proves P2 does not turn
// every same-process ErrSessionLeaseHeld into success. Only explicit
// maintenance generations are waitable; a live runtime remains protected and
// keeps the sanitized user-facing error plus structured diagnostics.
func TestCurrentProcessRuntimeHolderRemainsARealConflict(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	path := filepath.Join(dir, "issue-8372.jsonl")

	holder, err := agent.TryAcquireSessionLease(path)
	if err != nil {
		t.Fatalf("acquire maintenance holder: %v", err)
	}
	t.Cleanup(holder.Release)

	tab := &WorkspaceTab{ID: "tab-8372", Scope: "global", SessionPath: path}
	app := NewApp()
	app.tabs[tab.ID] = tab
	app.tabOrder = []string{tab.ID}
	t.Cleanup(tab.releaseSessionLease)

	started := time.Now()
	startupErr := app.ensureTabSessionLeaseForRebuild(tab, path, "")
	if elapsed := time.Since(started); elapsed > 250*time.Millisecond {
		t.Fatalf("runtime conflict waited %s instead of failing immediately", elapsed)
	}
	if !errors.Is(startupErr, agent.ErrSessionLeaseHeld) {
		t.Fatalf("startup error = %v, want ErrSessionLeaseHeld", startupErr)
	}
	if !strings.Contains(startupErr.Error(), "this session is already open in another Reasonix window") {
		t.Fatalf("startup error = %q, want sanitized desktop lease message", startupErr)
	}
	if strings.Contains(startupErr.Error(), path) || strings.Contains(startupErr.Error(), agent.SessionWriterID()) {
		t.Fatalf("startup error leaked internal lease diagnostics: %q", startupErr)
	}

	var leaseErr *agent.SessionLeaseError
	if !errors.As(startupErr, &leaseErr) || leaseErr == nil || leaseErr.Info == nil {
		t.Fatalf("wrapped startup error lost SessionLeaseError info: %#v", startupErr)
	}
	if leaseErr.Info.PID != os.Getpid() || leaseErr.Info.WriterID != agent.SessionWriterID() {
		t.Fatalf("holder = pid %d writer %q, want current process pid %d writer %q",
			leaseErr.Info.PID, leaseErr.Info.WriterID, os.Getpid(), agent.SessionWriterID())
	}

	issue := sessionRuntimeIssueForError(startupErr)
	if issue == nil || issue.Code != "session_lease_held" || !issue.Retryable {
		t.Fatalf("runtime issue = %#v, want retryable session_lease_held", issue)
	}
	if issue.HolderPID != os.Getpid() || strings.TrimSpace(issue.HolderHost) == "" || issue.AcquiredAt == "" {
		t.Fatalf("runtime issue holder diagnostics incomplete: %#v", issue)
	}
}

func TestMaintenanceLeaseWaitDoesNotRetryForeignHolder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "foreign-session.jsonl")
	foreignErr := &agent.SessionLeaseError{
		Path: path,
		Info: &agent.SessionLeaseInfo{
			SessionPath: path,
			WriterID:    "other-window-writer",
			PID:         os.Getpid() + 1,
			Hostname:    "other-host",
			AcquiredAt:  time.Now().UTC(),
		},
	}
	attempts := 0
	started := time.Now()
	_, err := withSessionMaintenanceLeaseWait(func() (struct{}, error) {
		attempts++
		return struct{}{}, foreignErr
	})
	if !errors.Is(err, agent.ErrSessionLeaseHeld) {
		t.Fatalf("foreign holder err = %v, want ErrSessionLeaseHeld", err)
	}
	if attempts != 1 {
		t.Fatalf("foreign holder attempts = %d, want exactly one", attempts)
	}
	if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
		t.Fatalf("foreign holder waited %s instead of failing immediately", elapsed)
	}
}

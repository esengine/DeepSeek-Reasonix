package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/agent"
)

func TestSessionOwnerActionsPerKind(t *testing.T) {
	cases := []struct {
		kind string
		want []string
	}{
		{sessionOwnerCurrentTab, []string{"focus"}},
		{sessionOwnerCurrentDetached, []string{"focus"}},
		{sessionOwnerSameHidden, []string{"focus"}},
		{sessionOwnerExternal, []string{"retry", "read_only", "copy"}},
		{sessionOwnerStale, []string{"retry"}},
		{sessionOwnerUnknown, []string{"read_only", "copy"}},
	}
	for _, tc := range cases {
		got := sessionOwnerActions(tc.kind)
		if len(got) != len(tc.want) {
			t.Errorf("actions(%s) = %v, want %v", tc.kind, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("actions(%s) = %v, want %v", tc.kind, got, tc.want)
				break
			}
		}
	}
}

func TestClassifySessionOwnerUnknownWithoutLeaseInfo(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "session.jsonl")
	kind, pid, _, _, _ := classifySessionOwner(path, nil, false)
	if kind != sessionOwnerUnknown {
		t.Fatalf("kind = %s, want unknown", kind)
	}
	if pid != 0 {
		t.Errorf("pid = %d, want 0", pid)
	}
}

func TestClassifySessionOwnerStaleWhenLockFree(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "session.jsonl")
	// Metadata recorded by a long-gone process with the OS lock free: the
	// probe reclaims the stale lease and classifies it as stale_reclaimed.
	leaseErr := &agent.SessionLeaseError{Info: &agent.SessionLeaseInfo{
		SessionPath: path,
		PID:         999999,
		Hostname:    "old-host",
	}}
	kind, pid, host, _, _ := classifySessionOwner(path, leaseErr, false)
	if kind != sessionOwnerStale {
		t.Fatalf("kind = %s, want stale_reclaimed", kind)
	}
	if pid != 999999 {
		t.Errorf("pid = %d, want recorded holder", pid)
	}
	if host != "old-host" {
		t.Errorf("host = %q", host)
	}
}

func TestResolveSessionIssueConsumesConcurrentActionOnce(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	app := NewApp()
	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(path, []byte("[]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	tab := &WorkspaceTab{ID: "tab-test", SessionPath: path}
	app.mu.Lock()
	app.tabs[tab.ID] = tab
	rt := app.newSessionRuntimeLocked(tab, sessionRuntimeKey(path))
	rt.Phase = sessionRuntimeLeaseBlocked
	rt.Issue = &SessionRuntimeIssue{
		Code:      "session_lease_held",
		IssueID:   "issue-1",
		Message:   "retry",
		Retryable: true,
		OwnerKind: sessionOwnerStale,
		Actions:   []string{"retry"},
		epoch:     rt.Epoch,
	}
	app.mu.Unlock()

	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	app.sessionIssueBeforeCommitHook = func() {
		entered <- struct{}{}
		<-release
	}
	var builds atomic.Int32
	app.sessionIssueBuildStarter = func(*WorkspaceTab) {
		builds.Add(1)
	}
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			errs <- app.ResolveSessionRuntimeIssue(tab.ID, "issue-1", "retry")
		}()
	}
	// Both calls have validated the same outer snapshot. Releasing them
	// together deterministically exercises the commit-time compare-and-consume.
	<-entered
	<-entered
	close(release)

	var successes, rejected int
	for i := 0; i < 2; i++ {
		err := <-errs
		switch {
		case err == nil:
			successes++
		case strings.Contains(err.Error(), "no longer current"):
			rejected++
		default:
			t.Fatalf("unexpected concurrent resolution error: %v", err)
		}
	}
	if successes != 1 || rejected != 1 {
		t.Fatalf("concurrent results: successes=%d rejected=%d, want 1/1", successes, rejected)
	}
	if got := builds.Load(); got != 1 {
		t.Fatalf("controller builds scheduled = %d, want 1", got)
	}
	app.mu.RLock()
	view := app.sessionRuntimeViewLocked(tab)
	app.mu.RUnlock()
	if view.Phase != sessionRuntimeStarting || view.Issue != nil {
		t.Fatalf("runtime after consume = %+v, want starting with no issue", view)
	}
}

func TestClassifySessionOwnerExternalProcess(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(path, []byte("[]"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A genuinely foreign holder: a child process acquires the OS lease.
	cmd := exec.Command(os.Args[0], "-test.run=TestSessionLeaseHelperProcess")
	cmd.Env = append(os.Environ(), "REASONIX_HOME="+home, "SESSION_LEASE_PATH="+path)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill() }()
	held := path + ".held"
	deadline := time.Now().Add(20 * time.Second)
	for {
		if _, err := os.Stat(held); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("child never acquired the lease")
		}
		time.Sleep(50 * time.Millisecond)
	}
	kind, _, _, _, _ := classifySessionOwner(path, nil, false)
	if kind != sessionOwnerExternal {
		t.Fatalf("kind = %s, want external_process", kind)
	}
}

// TestSessionLeaseHelperProcess is re-executed by the parent test to hold a
// session lease from a different process.
func TestSessionLeaseHelperProcess(t *testing.T) {
	path := os.Getenv("SESSION_LEASE_PATH")
	if path == "" {
		t.Skip("helper process only")
	}
	lease, err := agent.TryAcquireSessionLease(path)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	if err := os.WriteFile(path+".held", []byte("1"), 0o600); err != nil {
		t.Fatal(err)
	}
	time.Sleep(30 * time.Second)
}

package serve

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/provider"
)

func saveServeTestSession(t *testing.T, path string) {
	t.Helper()
	s := agent.NewSession("sys")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "hi"})
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
}

// TestResumeRefusedWhenSessionLeaseHeld proves POST /resume refuses to bind a
// session another runtime holds, keeps the server on its current session, and
// reports the shared holder wording.
func TestResumeRefusedWhenSessionLeaseHeld(t *testing.T) {
	dir := t.TempDir()
	active := filepath.Join(dir, "active.jsonl")
	held := filepath.Join(dir, "held.jsonl")
	saveServeTestSession(t, active)
	saveServeTestSession(t, held)

	holder, err := agent.TryAcquireSessionLease(held)
	if err != nil {
		t.Fatalf("test holder acquire: %v", err)
	}
	defer holder.Release()

	bc := NewBroadcaster()
	ctrl := control.New(control.Options{Sink: bc, SessionDir: dir, SessionPath: active})
	server := New(ctrl, bc, config.ServeConfig{})
	leases := control.NewSessionLeaseKeeper()
	defer leases.Release()
	if err := leases.Rebind(active); err != nil {
		t.Fatalf("seed lease on active: %v", err)
	}
	server.SetSessionLeases(leases)
	srv := httptest.NewServer(server.Handler())
	defer srv.Close()

	body, err := json.Marshal(map[string]string{"path": held})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post(srv.URL+"/resume", "application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	respBody, _ := readAll(resp)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("held resume status = %d, want 409 (body %q)", resp.StatusCode, respBody)
	}
	if !strings.Contains(respBody, "in use by another Reasonix") {
		t.Fatalf("held resume body = %q, want holder wording", respBody)
	}
	if strings.Contains(respBody, held) {
		t.Fatalf("held resume body leaks the session path: %q", respBody)
	}
	if got := filepath.Clean(ctrl.SessionPath()); got != filepath.Clean(active) {
		t.Fatalf("session path after refused resume = %q, want active %q", got, active)
	}
	if got, want := leases.HeldPath(), agent.CanonicalSessionPath(active); got != want {
		t.Fatalf("lease after refused resume = %q, want %q", got, want)
	}
}

// TestResumeMovesSessionLease proves a successful POST /resume releases the old
// session's lease and holds the new one.
func TestResumeMovesSessionLease(t *testing.T) {
	dir := t.TempDir()
	active := filepath.Join(dir, "active.jsonl")
	next := filepath.Join(dir, "next.jsonl")
	saveServeTestSession(t, active)
	saveServeTestSession(t, next)

	bc := NewBroadcaster()
	ctrl := control.New(control.Options{Sink: bc, SessionDir: dir, SessionPath: active})
	server := New(ctrl, bc, config.ServeConfig{})
	leases := control.NewSessionLeaseKeeper()
	defer leases.Release()
	if err := leases.Rebind(active); err != nil {
		t.Fatalf("seed lease on active: %v", err)
	}
	server.SetSessionLeases(leases)
	srv := httptest.NewServer(server.Handler())
	defer srv.Close()

	body, err := json.Marshal(map[string]string{"path": next})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post(srv.URL+"/resume", "application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	respBody, _ := readAll(resp)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("resume status = %d, want 204 (body %q)", resp.StatusCode, respBody)
	}
	want, err := filepath.EvalSymlinks(next)
	if err != nil {
		t.Fatal(err)
	}
	if got, wantHeld := leases.HeldPath(), agent.CanonicalSessionPath(want); got != wantHeld {
		t.Fatalf("lease after resume = %q, want %q", got, wantHeld)
	}
	lease, err := agent.TryAcquireSessionLease(active)
	if err != nil {
		t.Fatalf("old session lease not released by resume: %v", err)
	}
	lease.Release()
}

func readAll(resp *http.Response) (string, error) {
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	return strings.TrimSpace(string(b)), err
}

func postServeLeaseJSON(client *http.Client, url string, body any, want int) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	resp, err := client.Post(url, "application/json", strings.NewReader(string(payload)))
	if err != nil {
		return err
	}
	respBody, readErr := readAll(resp)
	if readErr != nil {
		return readErr
	}
	if resp.StatusCode != want {
		return fmt.Errorf("POST %s status = %d, want %d (body %q)", url, resp.StatusCode, want, respBody)
	}
	return nil
}

func waitServeLeaseDone(t *testing.T, done <-chan struct{}, what string, timeout time.Duration) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(timeout):
		t.Fatalf("timed out waiting for %s", what)
	}
}

func waitServeLeaseResult(t *testing.T, ch <-chan error, what string, timeout time.Duration) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-time.After(timeout):
		return fmt.Errorf("timed out waiting for %s", what)
	}
}

// TestConcurrentResumesKeepControllerAndLeaseAligned hammers POST /resume from
// two goroutines bouncing between different targets and asserts the invariant
// this PR exists for: whatever session the controller ends up writing is the
// session the lease keeper guards. Without bindMu serializing the
// snapshot→rebind→resume sequence, interleaved handlers split the two (the
// controller on one path, the lease on another), leaving the written session
// unprotected and a foreign one wrongly occupied.
func TestConcurrentResumesKeepControllerAndLeaseAligned(t *testing.T) {
	dir := t.TempDir()
	active := filepath.Join(dir, "active.jsonl")
	targetB := filepath.Join(dir, "target-b.jsonl")
	targetC := filepath.Join(dir, "target-c.jsonl")
	for _, p := range []string{active, targetB, targetC} {
		saveServeTestSession(t, p)
	}

	bc := NewBroadcaster()
	ctrl := control.New(control.Options{Sink: bc, SessionDir: dir, SessionPath: active})
	defer ctrl.Close()
	server := New(ctrl, bc, config.ServeConfig{})
	leases := control.NewSessionLeaseKeeper()
	defer leases.Release()
	if err := leases.Rebind(active); err != nil {
		t.Fatalf("seed lease on active: %v", err)
	}
	server.SetSessionLeases(leases)
	srv := httptest.NewServer(server.Handler())
	defer srv.Close()
	client := &http.Client{Timeout: 10 * time.Second}

	post := func(target string) error {
		return postServeLeaseJSON(client, srv.URL+"/resume", map[string]string{"path": target}, http.StatusNoContent)
	}

	const rounds = 25
	var wg sync.WaitGroup
	errs := make(chan error, rounds*2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		for range rounds {
			if err := post(targetB); err != nil {
				errs <- err
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		for range rounds {
			if err := post(targetC); err != nil {
				errs <- err
				return
			}
		}
	}()
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
		close(errs)
	}()
	waitServeLeaseDone(t, done, "concurrent resume posts", 20*time.Second)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}

	got := agent.CanonicalSessionPath(server.ctl().SessionPath())
	held := leases.HeldPath()
	if got != held {
		t.Fatalf("controller/lease split after concurrent resumes:\n  controller writes %q\n  lease guards      %q", got, held)
	}
}

// TestConcurrentResumeAndForkKeepAlignment interleaves /resume with /fork —
// the two handlers rotate the active path through different code paths — and
// asserts the same controller/lease alignment invariant.
func TestConcurrentResumeAndForkKeepAlignment(t *testing.T) {
	dir := t.TempDir()
	active := filepath.Join(dir, "active.jsonl")
	target := filepath.Join(dir, "target.jsonl")
	saveServeTestSession(t, active)
	saveServeTestSession(t, target)

	bc := NewBroadcaster()
	ctrl := control.New(control.Options{Sink: bc, SessionDir: dir, SessionPath: active})
	defer ctrl.Close()
	server := New(ctrl, bc, config.ServeConfig{})
	leases := control.NewSessionLeaseKeeper()
	defer leases.Release()
	if err := leases.Rebind(active); err != nil {
		t.Fatalf("seed lease on active: %v", err)
	}
	server.SetSessionLeases(leases)
	srv := httptest.NewServer(server.Handler())
	defer srv.Close()
	client := &http.Client{Timeout: 10 * time.Second}

	var wg sync.WaitGroup
	errs := make(chan error, 30)
	wg.Add(2)
	go func() {
		defer wg.Done()
		for range 15 {
			if err := postServeLeaseJSON(client, srv.URL+"/resume", map[string]string{"path": target}, http.StatusNoContent); err != nil {
				errs <- err
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		for range 15 {
			payload, _ := json.Marshal(map[string]any{"turn": 0, "name": ""})
			resp, err := client.Post(srv.URL+"/fork", "application/json", strings.NewReader(string(payload)))
			if err != nil {
				errs <- err
				return
			}
			_, readErr := readAll(resp)
			if readErr != nil {
				errs <- readErr
				return
			}
		}
	}()
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
		close(errs)
	}()
	waitServeLeaseDone(t, done, "concurrent resume/fork posts", 20*time.Second)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}

	got := agent.CanonicalSessionPath(server.ctl().SessionPath())
	held := leases.HeldPath()
	if got != held {
		t.Fatalf("controller/lease split after resume×fork interleave:\n  controller writes %q\n  lease guards      %q", got, held)
	}
}

// TestInterleavedResumesForcedThroughBindWindow deterministically forces the
// interleaving bindMu prevents: request 1 is parked between its lease rebind
// and its controller Resume while request 2 tries to resume a different
// session. With bindMu, request 2 waits and both requests land aligned;
// without it, request 2 completes inside request 1's window and request 1
// then rebinds the controller to a session the lease no longer guards.
func TestInterleavedResumesForcedThroughBindWindow(t *testing.T) {
	dir := t.TempDir()
	active := filepath.Join(dir, "active.jsonl")
	targetB := filepath.Join(dir, "target-b.jsonl")
	targetC := filepath.Join(dir, "target-c.jsonl")
	for _, p := range []string{active, targetB, targetC} {
		saveServeTestSession(t, p)
	}

	bc := NewBroadcaster()
	ctrl := control.New(control.Options{Sink: bc, SessionDir: dir, SessionPath: active})
	defer ctrl.Close()
	server := New(ctrl, bc, config.ServeConfig{})
	leases := control.NewSessionLeaseKeeper()
	defer leases.Release()
	if err := leases.Rebind(active); err != nil {
		t.Fatalf("seed lease on active: %v", err)
	}
	server.SetSessionLeases(leases)
	srv := httptest.NewServer(server.Handler())
	defer srv.Close()
	client := &http.Client{Timeout: 10 * time.Second}

	entered := make(chan struct{})
	release := make(chan struct{})
	// Park only the FIRST request through the hook. A sync.Once would not do:
	// Once.Do holds an internal mutex while f runs, so the second request
	// would block on the Once itself and accidentally reproduce bindMu's
	// serialization even with the guard removed.
	var first atomic.Bool
	first.Store(true)
	resumeBindHookForTest = func() {
		if first.CompareAndSwap(true, false) {
			close(entered)
			<-release
		}
	}
	defer func() { resumeBindHookForTest = nil }()
	var releaseOnce sync.Once
	unpark := func() {
		releaseOnce.Do(func() {
			close(release)
		})
	}
	t.Cleanup(unpark)

	post := func(target string) error {
		return postServeLeaseJSON(client, srv.URL+"/resume", map[string]string{"path": target}, http.StatusNoContent)
	}

	done1 := make(chan error, 1)
	go func() { done1 <- post(targetB) }()
	select {
	case <-entered:
		// Request 1 parked inside its bind window, lease moved to B.
	case err := <-done1:
		if err != nil {
			t.Fatalf("first resume finished before entering bind hook: %v", err)
		}
		t.Fatal("first resume finished before entering bind hook")
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for first resume to enter bind hook")
	}

	done2 := make(chan error, 1)
	go func() { done2 <- post(targetC) }()
	// Request 2 must not complete while request 1 owns the bind window.
	select {
	case err := <-done2:
		// Unpark request 1 before failing, or httptest's Close would wait the
		// full package timeout on the parked handler.
		unpark()
		if waitErr := waitServeLeaseResult(t, done1, "first resume after failed serialization check", 5*time.Second); waitErr != nil {
			t.Fatalf("second resume completed inside the first resume's bind window; first resume cleanup failed: %v", waitErr)
		}
		if err != nil {
			t.Fatalf("second resume completed inside the first resume's bind window with error: %v", err)
		}
		t.Fatal("second resume completed inside the first resume's bind window")
	case <-time.After(150 * time.Millisecond):
	}

	unpark()
	if err := waitServeLeaseResult(t, done1, "first resume", 5*time.Second); err != nil {
		t.Fatal(err)
	}
	if err := waitServeLeaseResult(t, done2, "second resume", 5*time.Second); err != nil {
		t.Fatal(err)
	}

	got := agent.CanonicalSessionPath(server.ctl().SessionPath())
	held := leases.HeldPath()
	if got != held {
		t.Fatalf("controller/lease split after forced interleave:\n  controller writes %q\n  lease guards      %q", got, held)
	}
	wantC := targetC
	if resolved, err := filepath.EvalSymlinks(targetC); err == nil {
		wantC = resolved // serve resolves the request path; match its form
	}
	if got != agent.CanonicalSessionPath(wantC) {
		t.Fatalf("last resume should win: controller on %q, want %q", got, wantC)
	}
}

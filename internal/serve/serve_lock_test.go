package serve

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/jobs"
)

type blockingRequestBody struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (b *blockingRequestBody) Read([]byte) (int, error) {
	b.once.Do(func() { close(b.started) })
	<-b.release
	return 0, io.EOF
}

func (*blockingRequestBody) Close() error { return nil }

// lockProbeController wraps a real controller but intercepts the two blocking
// steps of a model switch — Snapshot (may touch disk) and Close (jobs grace wait
// up to 15s + SessionEnd hook) — so a test can assert switchModel runs them while
// s.mu is free. Embedding *control.Controller keeps it a full SessionAPI.
type lockProbeController struct {
	*control.Controller
	onSnapshot func()
	onClose    func()
}

type blockingNewSessionController struct {
	*control.Controller
	entered chan struct{}
	release chan struct{}
}

func (c *blockingNewSessionController) NewSession() error {
	close(c.entered)
	<-c.release
	return c.Controller.NewSession()
}

func (c *lockProbeController) Snapshot() error {
	if c.onSnapshot != nil {
		c.onSnapshot()
	}
	return c.Controller.Snapshot()
}

func (c *lockProbeController) Close() {
	if c.onClose != nil {
		c.onClose()
	}
	c.Controller.Close()
}

// expectServerMutexAvailable returns a callback that fails the test if s.mu can't
// be acquired within 500ms — i.e. switchModel is holding the lock across the
// callback. It signals checks once it has probed, so the test can assert the
// callback actually ran.
func expectServerMutexAvailable(t *testing.T, s *Server, checks chan<- struct{}) func() {
	t.Helper()
	return func() {
		acquired := make(chan struct{})
		go func() {
			s.mu.Lock()
			s.mu.Unlock() //nolint:staticcheck // probe: lock must be immediately acquirable
			close(acquired)
		}()
		select {
		case <-acquired:
		case <-time.After(500 * time.Millisecond):
			t.Error("switchModel held s.mu across a Snapshot/Close callback")
		}
		if checks == nil {
			return
		}
		select {
		case checks <- struct{}{}:
		default:
		}
	}
}

// TestSwitchModelDoesNotHoldServerLockDuringSnapshotAndClose is the regression
// guard for the serve.go:114 lock-audit fix: Snapshot on the old controller,
// boot.Build of the new one, and Close of the old one must all run OFF s.mu so
// HTTP handlers blocked on s.ctl()'s RLock aren't stalled (worst case 15s+ on
// Close). The probe callbacks try to grab s.mu on another goroutine and fail
// fast if it's held.
func TestSwitchModelDoesNotHoldServerLockDuringSnapshotAndClose(t *testing.T) {
	bc := NewBroadcaster()
	snapChecks := make(chan struct{}, 1)
	closeChecks := make(chan struct{}, 1)

	old := &lockProbeController{Controller: control.New(control.Options{Sink: bc})}
	s := &Server{ctrl: old, bc: bc}
	old.onSnapshot = expectServerMutexAvailable(t, s, snapChecks)
	old.onClose = expectServerMutexAvailable(t, s, closeChecks)

	var built *control.Controller
	s.buildController = func(_ context.Context, _ string) (*control.Controller, error) {
		built = control.New(control.Options{Sink: bc})
		return built, nil
	}

	if err := s.switchModel(context.Background(), "next-model"); err != nil {
		t.Fatalf("switchModel: %v", err)
	}

	select {
	case <-snapChecks:
	case <-time.After(time.Second):
		t.Fatal("Snapshot callback never ran during switchModel")
	}
	select {
	case <-closeChecks:
	case <-time.After(time.Second):
		t.Fatal("Close callback never ran during switchModel")
	}
	if s.ctl() != built {
		t.Fatal("switchModel did not publish the freshly built controller")
	}
}

// TestSwitchModelDiscardsBuiltControllerOnConcurrentSwap verifies the failure
// path: if the controller is swapped out (e.g. by resume) between Build and the
// publish lock, switchModel must discard the new controller instead of leaking
// it or clobbering the concurrent swap.
func TestSwitchModelDiscardsBuiltControllerOnConcurrentSwap(t *testing.T) {
	bc := NewBroadcaster()
	old := control.New(control.Options{Sink: bc})
	other := control.New(control.Options{Sink: bc})
	s := &Server{ctrl: old, bc: bc}

	var built *control.Controller
	s.buildController = func(_ context.Context, _ string) (*control.Controller, error) {
		// Simulate a concurrent path (resume/new-session) replacing the
		// controller after the off-lock snapshot but before the publish lock.
		s.mu.Lock()
		s.ctrl = other
		s.mu.Unlock()
		built = control.New(control.Options{Sink: bc})
		return built, nil
	}

	err := s.switchModel(context.Background(), "next-model")
	if err == nil {
		t.Fatal("expected switchModel to fail when the controller changed mid-switch")
	}
	if s.ctl() != other {
		t.Fatal("switchModel clobbered a concurrent controller swap")
	}
}

// TestSwitchModelRejectsWhileRunning keeps the pre-existing guard: a switch is
// refused while a turn is running, before any snapshot/build work.
func TestSwitchModelRejectsWhileRunning(t *testing.T) {
	bc := NewBroadcaster()
	ctrl := control.New(control.Options{Runner: blockingRunner{}, Sink: bc})
	s := &Server{ctrl: ctrl, bc: bc}
	built := false
	s.buildController = func(_ context.Context, _ string) (*control.Controller, error) {
		built = true
		return control.New(control.Options{Sink: bc}), nil
	}

	// Drive the controller into a running turn.
	ctrl.SubmitHTTP("hi")
	waitRunning(t, ctrl)

	if err := s.switchModel(context.Background(), "next-model"); err == nil {
		t.Fatal("expected switchModel to refuse while a turn is running")
	}
	if built {
		t.Fatal("switchModel built a controller despite a running turn")
	}
	ctrl.Cancel()
	waitNotRunning(t, ctrl)
}

func TestForegroundMutationRejectsStaleSessionPath(t *testing.T) {
	dir := t.TempDir()
	currentPath := filepath.Join(dir, "current.jsonl")
	stalePath := filepath.Join(dir, "stale.jsonl")
	bc := NewBroadcaster()
	ctrl := control.New(control.Options{Runner: blockingRunner{}, Sink: bc, SessionPath: currentPath})
	s := New(ctrl, bc, config.ServeConfig{})

	ctrl.SubmitHTTP("hi")
	waitRunning(t, ctrl)
	req := httptest.NewRequest(http.MethodPost, "/cancel", strings.NewReader(`{}`))
	req.Host = "127.0.0.1"
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(expectedSessionPathHeader, stalePath)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("stale cancel status = %d, want %d", rec.Code, http.StatusConflict)
	}
	if !ctrl.Running() {
		t.Fatal("stale cancel reached the newly current controller")
	}

	req = httptest.NewRequest(http.MethodPost, "/cancel", strings.NewReader(`{}`))
	req.Host = "127.0.0.1"
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(expectedSessionPathHeader, currentPath)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("current cancel status = %d, want %d", rec.Code, http.StatusNoContent)
	}
	waitNotRunning(t, ctrl)
}

func TestForegroundMutationReadsBodyBeforeBindingLock(t *testing.T) {
	s := New(control.New(control.Options{}), NewBroadcaster(), config.ServeConfig{})
	blockedBody := &blockingRequestBody{started: make(chan struct{}), release: make(chan struct{})}
	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		req := httptest.NewRequest(http.MethodPost, "/slow", blockedBody)
		s.foregroundMutation(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.ReadAll(r.Body)
			w.WriteHeader(http.StatusNoContent)
		})(httptest.NewRecorder(), req)
	}()
	select {
	case <-blockedBody.started:
	case <-time.After(time.Second):
		t.Fatal("slow request body was never read")
	}

	secondDone := make(chan struct{})
	go func() {
		defer close(secondDone)
		req := httptest.NewRequest(http.MethodPost, "/fast", http.NoBody)
		s.foregroundMutation(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		})(httptest.NewRecorder(), req)
	}()
	select {
	case <-secondDone:
	case <-time.After(500 * time.Millisecond):
		close(blockedBody.release)
		<-firstDone
		t.Fatal("slow body held the session binding lock")
	}
	close(blockedBody.release)
	select {
	case <-firstDone:
	case <-time.After(time.Second):
		t.Fatal("slow request did not finish after its body was released")
	}
}

func TestForegroundMutationRejectsOversizedBodyBeforeHandler(t *testing.T) {
	s := New(control.New(control.Options{}), NewBroadcaster(), config.ServeConfig{})
	called := false
	req := httptest.NewRequest(http.MethodPost, "/oversized", strings.NewReader(strings.Repeat("x", foregroundMutationMaxBody+1)))
	rec := httptest.NewRecorder()
	s.foregroundMutation(func(http.ResponseWriter, *http.Request) { called = true })(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized body status = %d, want %d", rec.Code, http.StatusRequestEntityTooLarge)
	}
	if called {
		t.Fatal("oversized body reached the foreground handler")
	}
}

func TestSwitchModelRejectsWhileBackgroundJobRunning(t *testing.T) {
	bc := NewBroadcaster()
	manager := jobs.NewManager(bc)
	ctrl := control.New(control.Options{Sink: bc, Jobs: manager})
	defer ctrl.Close()
	manager.Start("task", "running", func(ctx context.Context, _ io.Writer) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	})

	s := &Server{ctrl: ctrl, bc: bc}
	built := false
	s.buildController = func(_ context.Context, _ string) (*control.Controller, error) {
		built = true
		return control.New(control.Options{Sink: bc}), nil
	}

	if err := s.switchModel(context.Background(), "next-model"); err == nil {
		t.Fatal("expected switchModel to refuse while a background job is running")
	}
	if built {
		t.Fatal("switchModel built a controller despite a running background job")
	}
}

func TestExtensionReloadRejectsWhileTurnRunning(t *testing.T) {
	bc := NewBroadcaster()
	ctrl := control.New(control.Options{Runner: blockingRunner{}, Sink: bc})
	s := New(ctrl, bc, config.ServeConfig{})
	built := false
	s.rebuildController = func(_ context.Context, _ *control.Controller, _ string) (*control.Controller, error) {
		built = true
		return control.New(control.Options{Sink: bc}), nil
	}

	ctrl.SubmitHTTP("hi")
	waitRunning(t, ctrl)
	if err := s.reloadExtensions(context.Background()); err == nil {
		t.Fatal("expected extension reload to refuse while a turn is running")
	}
	if built {
		t.Fatal("extension reload built a controller despite a running turn")
	}
	ctrl.Cancel()
	waitNotRunning(t, ctrl)
}

func TestConcurrentExtensionReloadsAreSerialized(t *testing.T) {
	bc := NewBroadcaster()
	s := New(control.New(control.Options{Sink: bc}), bc, config.ServeConfig{})
	firstEntered := make(chan struct{})
	secondEntered := make(chan struct{})
	releaseFirst := make(chan struct{})
	var calls atomic.Int32
	s.rebuildController = func(_ context.Context, _ *control.Controller, _ string) (*control.Controller, error) {
		if calls.Add(1) == 1 {
			close(firstEntered)
			<-releaseFirst
		} else {
			close(secondEntered)
		}
		return control.New(control.Options{Sink: bc}), nil
	}

	done := make(chan error, 2)
	go func() { done <- s.reloadExtensions(context.Background()) }()
	<-firstEntered
	go func() { done <- s.reloadExtensions(context.Background()) }()
	select {
	case <-secondEntered:
		t.Fatal("second extension reload entered the rebuild while the first still owned bindMu")
	case <-time.After(100 * time.Millisecond):
	}
	close(releaseFirst)
	for range 2 {
		if err := <-done; err != nil {
			t.Fatalf("reload: %v", err)
		}
	}
	if calls.Load() != 2 {
		t.Fatalf("rebuild calls = %d, want 2", calls.Load())
	}
}

func TestSubmitWaitsForExtensionReloadAndTargetsReplacement(t *testing.T) {
	bc := NewBroadcaster()
	old := control.New(control.Options{Sink: bc, Runner: blockingRunner{}})
	s := New(old, bc, config.ServeConfig{})
	buildEntered := make(chan struct{})
	releaseBuild := make(chan struct{})
	replacement := control.New(control.Options{Sink: bc, Runner: blockingRunner{}})
	s.rebuildController = func(_ context.Context, _ *control.Controller, _ string) (*control.Controller, error) {
		close(buildEntered)
		<-releaseBuild
		return replacement, nil
	}

	reloadDone := make(chan error, 1)
	go func() { reloadDone <- s.reloadExtensions(context.Background()) }()
	<-buildEntered

	submitDone := make(chan int, 1)
	go func() {
		req := httptest.NewRequest(http.MethodPost, "/submit", strings.NewReader(`{"input":"hello"}`))
		rec := httptest.NewRecorder()
		s.submit(rec, req)
		submitDone <- rec.Code
	}()
	select {
	case <-submitDone:
		t.Fatal("submit crossed the extension reload generation boundary")
	case <-time.After(100 * time.Millisecond):
	}

	close(releaseBuild)
	if err := <-reloadDone; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if code := <-submitDone; code != 202 {
		t.Fatalf("submit status = %d, want 202", code)
	}
	waitRunning(t, replacement)
	if old.Running() {
		t.Fatal("submit started on the outgoing controller")
	}
	replacement.Cancel()
	waitNotRunning(t, replacement)
}

func TestSubmitNewHoldsBindingLockUntilRotationCompletes(t *testing.T) {
	bc := NewBroadcaster()
	ctrl := &blockingNewSessionController{
		Controller: control.New(control.Options{Sink: bc, SessionDir: t.TempDir()}),
		entered:    make(chan struct{}),
		release:    make(chan struct{}),
	}
	t.Cleanup(func() {
		select {
		case <-ctrl.release:
		default:
			close(ctrl.release)
		}
	})
	s := New(ctrl, bc, config.ServeConfig{})
	submitDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		req := httptest.NewRequest(http.MethodPost, "/submit", strings.NewReader(`{"input":"/new"}`))
		rec := httptest.NewRecorder()
		s.submit(rec, req)
		submitDone <- rec
	}()
	select {
	case <-ctrl.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("/submit /new did not enter synchronous rotation")
	}
	lockAcquired := make(chan struct{})
	go func() {
		s.bindMu.Lock()
		close(lockAcquired)
		s.bindMu.Unlock()
	}()
	select {
	case <-lockAcquired:
		t.Fatal("bindMu was released before /new finished")
	case <-time.After(100 * time.Millisecond):
	}
	close(ctrl.release)
	var rec *httptest.ResponseRecorder
	select {
	case rec = <-submitDone:
	case <-time.After(2 * time.Second):
		t.Fatal("/submit /new did not return after rotation finished")
	}
	if rec.Code != http.StatusNoContent {
		t.Fatalf("/submit /new status = %d, want 204", rec.Code)
	}
	select {
	case <-lockAcquired:
	case <-time.After(2 * time.Second):
		t.Fatal("bindMu stayed locked after /new completed")
	}
}

func TestSessionSnapshotEndpointsWaitForBindingEpoch(t *testing.T) {
	bc := NewBroadcaster()
	ctrl := control.New(control.Options{Sink: bc, SessionDir: t.TempDir()})
	s := New(ctrl, bc, config.ServeConfig{})

	for _, endpoint := range []struct {
		name    string
		handler func(http.ResponseWriter, *http.Request)
	}{
		{name: "history", handler: s.history},
		{name: "status", handler: s.status},
	} {
		t.Run(endpoint.name, func(t *testing.T) {
			s.bindMu.Lock()
			done := make(chan struct{})
			go func() {
				rec := httptest.NewRecorder()
				req := httptest.NewRequest(http.MethodGet, "/"+endpoint.name+"?runtime=1", nil)
				endpoint.handler(rec, req)
				close(done)
			}()
			select {
			case <-done:
				s.bindMu.Unlock()
				t.Fatalf("/%s observed a controller snapshot during an active binding epoch", endpoint.name)
			case <-time.After(100 * time.Millisecond):
			}
			s.bindMu.Unlock()
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatalf("/%s stayed blocked after the binding epoch completed", endpoint.name)
			}
		})
	}
}

// blockingRunner keeps a turn "running" until its context is cancelled, so tests
// can observe Running() == true deterministically.
type blockingRunner struct{}

func (blockingRunner) Run(ctx context.Context, _ string) error {
	<-ctx.Done()
	return ctx.Err()
}

func waitRunning(t *testing.T, ctrl *control.Controller) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		if ctrl.Running() {
			return
		}
		select {
		case <-deadline:
			t.Fatal("controller never entered the running state")
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func waitNotRunning(t *testing.T, ctrl *control.Controller) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		if !ctrl.Running() {
			return
		}
		select {
		case <-deadline:
			t.Fatal("controller never left the running state after cancel")
		case <-time.After(5 * time.Millisecond):
		}
	}
}

package serve

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/jobs"
)

// lockProbeController wraps a real controller but intercepts the two blocking
// steps of a model switch — Snapshot (may touch disk) and Close (jobs grace wait
// up to 15s + SessionEnd hook) — so a test can assert switchModel runs them while
// s.mu is free. Embedding *control.Controller keeps it a full SessionAPI.
type lockProbeController struct {
	*control.Controller
	onSnapshot func()
	onClose    func()
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

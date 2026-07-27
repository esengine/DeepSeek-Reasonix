package serve

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/jobs"
	"reasonix/internal/sandboxauth"
)

type signalingReader struct {
	reader io.Reader
	read   chan struct{}
	once   sync.Once
}

func (r *signalingReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	if n > 0 {
		r.once.Do(func() { close(r.read) })
	}
	return n, err
}

func mutationRequest(body string) (*httptest.ResponseRecorder, *httpRequestSignal) {
	read := make(chan struct{})
	reader := &signalingReader{reader: bytes.NewBufferString(body), read: read}
	req := httptest.NewRequest("POST", "/", io.NopCloser(reader))
	return httptest.NewRecorder(), &httpRequestSignal{request: req, read: read}
}

type httpRequestSignal struct {
	request *http.Request
	read    <-chan struct{}
}

// lockProbeController wraps a real controller but intercepts the two blocking
// steps of a model switch — Snapshot (may touch disk) and Close (jobs grace wait
// up to 15s + SessionEnd hook) — so a test can assert switchModel runs them while
// s.mu is free. Embedding *control.Controller keeps it a full SessionAPI.
type lockProbeController struct {
	*control.Controller
	onSnapshot func()
	onClose    func()
}

type approvalResultController struct {
	*control.Controller
	resolveResult bool
	resolveCalls  chan string
}

func (c *approvalResultController) ResolveApproval(id string, _, _, _ bool) bool {
	if c.resolveCalls != nil {
		c.resolveCalls <- id
	}
	return c.resolveResult
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

func TestSwitchModelSerializesToolModeMutationOntoReplacement(t *testing.T) {
	bc := NewBroadcaster()
	old := control.New(control.Options{Sink: bc})
	s := &Server{ctrl: old, bc: bc}
	buildEntered := make(chan struct{})
	releaseBuild := make(chan struct{})
	var built *control.Controller
	s.buildController = func(_ context.Context, _ string) (*control.Controller, error) {
		close(buildEntered)
		<-releaseBuild
		built = control.New(control.Options{Sink: bc})
		return built, nil
	}

	switchDone := make(chan error, 1)
	go func() { switchDone <- s.switchModel(context.Background(), "next-model") }()
	<-buildEntered

	recorder, signaled := mutationRequest(`{"mode":"yolo"}`)
	handlerDone := make(chan struct{})
	go func() {
		s.toolApprovalMode(recorder, signaled.request)
		close(handlerDone)
	}()
	<-signaled.read
	select {
	case <-handlerDone:
		t.Fatal("tool mode mutation completed against the outgoing controller during build")
	case <-time.After(25 * time.Millisecond):
	}

	close(releaseBuild)
	if err := <-switchDone; err != nil {
		t.Fatalf("switchModel: %v", err)
	}
	select {
	case <-handlerDone:
	case <-time.After(time.Second):
		t.Fatal("tool mode mutation did not resume after controller publication")
	}
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("tool mode status = %d, want 204", recorder.Code)
	}
	if s.ctl() != built || built.ToolApprovalMode() != control.ToolApprovalYolo {
		t.Fatalf("replacement mode = %q, want yolo", built.ToolApprovalMode())
	}
}

func TestSwitchModelDoesNotFalselyResolveCapabilityOnOutgoingController(t *testing.T) {
	ids := make(chan string, 1)
	old := control.New(control.Options{Sink: event.FuncSink(func(e event.Event) {
		if e.Kind == event.ApprovalRequest && e.Approval.Kind == sandboxauth.ApprovalKind {
			ids <- e.Approval.ID
		}
	})})
	old.EnableInteractiveApproval()

	bc := NewBroadcaster()
	s := &Server{ctrl: old, bc: bc}
	buildEntered := make(chan struct{})
	releaseBuild := make(chan struct{})
	s.buildController = func(_ context.Context, _ string) (*control.Controller, error) {
		close(buildEntered)
		<-releaseBuild
		return control.New(control.Options{Sink: bc}), nil
	}
	switchDone := make(chan error, 1)
	go func() { switchDone <- s.switchModel(context.Background(), "next-model") }()
	<-buildEntered

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _, _ = old.ApproveSandboxCapability(ctx, sandboxauth.Prompt{}) }()
	id := <-ids

	recorder, signaled := mutationRequest(`{"id":"` + id + `","action":"run_sandboxed"}`)
	handlerDone := make(chan struct{})
	go func() {
		s.capabilityApprove(recorder, signaled.request)
		close(handlerDone)
	}()
	<-signaled.read
	select {
	case <-handlerDone:
		t.Fatal("capability decision resolved on the outgoing controller during build")
	case <-time.After(25 * time.Millisecond):
	}

	close(releaseBuild)
	if err := <-switchDone; err != nil {
		t.Fatalf("switchModel: %v", err)
	}
	select {
	case <-handlerDone:
	case <-time.After(time.Second):
		t.Fatal("capability decision did not resume after controller publication")
	}
	if recorder.Code != http.StatusConflict {
		t.Fatalf("stale capability status = %d, want 409 instead of false success", recorder.Code)
	}
}

func TestSwitchModelSerializesSubmitOntoReplacement(t *testing.T) {
	bc := NewBroadcaster()
	oldInputs := make(chan string, 1)
	old := control.New(control.Options{Runner: fakeRunner{got: oldInputs}, Sink: bc})
	s := &Server{ctrl: old, bc: bc}

	buildEntered := make(chan struct{})
	releaseBuild := make(chan struct{})
	replacementInputs := make(chan string, 1)
	var replacement *control.Controller
	s.buildController = func(_ context.Context, _ string) (*control.Controller, error) {
		close(buildEntered)
		<-releaseBuild
		replacement = control.New(control.Options{Runner: fakeRunner{got: replacementInputs}, Sink: bc})
		return replacement, nil
	}

	switchDone := make(chan error, 1)
	go func() { switchDone <- s.switchModel(context.Background(), "next-model") }()
	<-buildEntered

	recorder, signaled := mutationRequest(`{"input":"late input"}`)
	submitDone := make(chan struct{})
	go func() {
		s.submit(recorder, signaled.request)
		close(submitDone)
	}()
	<-signaled.read
	select {
	case <-submitDone:
		t.Fatal("submit completed against the outgoing controller during build")
	case <-time.After(25 * time.Millisecond):
	}

	close(releaseBuild)
	if err := <-switchDone; err != nil {
		t.Fatalf("switchModel: %v", err)
	}
	select {
	case <-submitDone:
	case <-time.After(time.Second):
		t.Fatal("submit did not resume after controller publication")
	}
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("submit status = %d, want 202", recorder.Code)
	}
	select {
	case got := <-replacementInputs:
		if got != "late input" {
			t.Fatalf("replacement input = %q, want late input", got)
		}
	case <-time.After(time.Second):
		t.Fatal("replacement controller did not receive submitted input")
	}
	select {
	case got := <-oldInputs:
		t.Fatalf("outgoing controller received submitted input %q", got)
	default:
	}
}

func TestSwitchModelSerializesGoalMutationOntoReplacement(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		prepare  func(*control.Controller)
		wantGoal string
		wantPlan bool
	}{
		{
			name: "set disables plan",
			body: `{"goal":"ship issue seven"}`,
			prepare: func(ctrl *control.Controller) {
				ctrl.SetPlanMode(true)
			},
			wantGoal: "ship issue seven",
			wantPlan: false,
		},
		{
			name: "clear preserves plan",
			body: `{"goal":""}`,
			prepare: func(ctrl *control.Controller) {
				ctrl.SetPlanMode(true)
				ctrl.SetGoal("replacement goal")
			},
			wantPlan: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			bc := NewBroadcaster()
			old := control.New(control.Options{Sink: bc})
			old.SetPlanMode(true)
			s := &Server{ctrl: old, bc: bc}
			buildEntered := make(chan struct{})
			releaseBuild := make(chan struct{})
			var built *control.Controller
			s.buildController = func(_ context.Context, _ string) (*control.Controller, error) {
				close(buildEntered)
				<-releaseBuild
				built = control.New(control.Options{Sink: bc})
				tc.prepare(built)
				return built, nil
			}

			switchDone := make(chan error, 1)
			go func() { switchDone <- s.switchModel(context.Background(), "next-model") }()
			<-buildEntered

			recorder, signaled := mutationRequest(tc.body)
			handlerDone := make(chan struct{})
			go func() {
				s.goal(recorder, signaled.request)
				close(handlerDone)
			}()
			<-signaled.read
			close(releaseBuild)
			if err := <-switchDone; err != nil {
				t.Fatalf("switchModel: %v", err)
			}
			select {
			case <-handlerDone:
			case <-time.After(time.Second):
				t.Fatal("goal mutation did not resume after controller publication")
			}
			if recorder.Code != http.StatusNoContent {
				t.Fatalf("goal status = %d, want 204", recorder.Code)
			}
			if got := built.Goal(); got != tc.wantGoal {
				t.Fatalf("replacement goal = %q, want %q", got, tc.wantGoal)
			}
			if got := built.PlanMode(); got != tc.wantPlan {
				t.Fatalf("replacement plan = %v, want %v", got, tc.wantPlan)
			}
		})
	}
}

func TestApproveHandlerReportsResolutionResult(t *testing.T) {
	for _, tc := range []struct {
		name       string
		resolved   bool
		wantStatus int
	}{
		{name: "live", resolved: true, wantStatus: http.StatusNoContent},
		{name: "stale", resolved: false, wantStatus: http.StatusConflict},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := &approvalResultController{
				Controller:    control.New(control.Options{}),
				resolveResult: tc.resolved,
				resolveCalls:  make(chan string, 1),
			}
			s := &Server{ctrl: ctrl}
			recorder := httptest.NewRecorder()
			s.approve(recorder, httptest.NewRequest("POST", "/approve", bytes.NewBufferString(`{"id":"approval-1","allow":true}`)))
			if recorder.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", recorder.Code, tc.wantStatus)
			}
			if got := <-ctrl.resolveCalls; got != "approval-1" {
				t.Fatalf("resolved id = %q", got)
			}
		})
	}
}

func TestSwitchModelRejectsLegacyApprovalStaleOnReplacement(t *testing.T) {
	bc := NewBroadcaster()
	old := &approvalResultController{
		Controller:    control.New(control.Options{Sink: bc}),
		resolveResult: true,
		resolveCalls:  make(chan string, 1),
	}
	s := &Server{ctrl: old, bc: bc}
	buildEntered := make(chan struct{})
	releaseBuild := make(chan struct{})
	s.buildController = func(_ context.Context, _ string) (*control.Controller, error) {
		close(buildEntered)
		<-releaseBuild
		return control.New(control.Options{Sink: bc}), nil
	}
	switchDone := make(chan error, 1)
	go func() { switchDone <- s.switchModel(context.Background(), "next-model") }()
	<-buildEntered

	recorder, signaled := mutationRequest(`{"id":"old-approval","allow":true}`)
	handlerDone := make(chan struct{})
	go func() {
		s.approve(recorder, signaled.request)
		close(handlerDone)
	}()
	<-signaled.read
	close(releaseBuild)
	if err := <-switchDone; err != nil {
		t.Fatalf("switchModel: %v", err)
	}
	select {
	case <-handlerDone:
	case <-time.After(time.Second):
		t.Fatal("legacy approval did not resume after controller publication")
	}
	if recorder.Code != http.StatusConflict {
		t.Fatalf("stale approval status = %d, want 409", recorder.Code)
	}
	select {
	case id := <-old.resolveCalls:
		t.Fatalf("approval %q was resolved on outgoing controller", id)
	default:
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

package host

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/remote/idempotency"
	"reasonix/internal/remote/protocol"
)

type recoveryAwareTestFactory struct {
	delegate *fakeControllerFactory
	state    control.SessionResumeState

	mu            sync.Mutex
	recoveryCalls int
}

func (f *recoveryAwareTestFactory) CreateController(ctx context.Context, target protocol.RuntimeTarget, sink event.Sink) (control.SessionAPI, error) {
	return f.delegate.CreateController(ctx, target, sink)
}

func (f *recoveryAwareTestFactory) CreateControllerWithRecovery(ctx context.Context, target protocol.RuntimeTarget, sink event.Sink) (control.SessionAPI, control.SessionResumeState, error) {
	controller, err := f.delegate.CreateController(ctx, target, sink)
	f.mu.Lock()
	f.recoveryCalls++
	f.mu.Unlock()
	return controller, f.state, err
}

type orderedShutdownController struct {
	*fakeSessionController
	prepared          chan struct{}
	prepareOnce       sync.Once
	contextStillAlive atomic.Bool
}

func (c *orderedShutdownController) ResumeWithRecovery(_ *agent.Session, _ string) (control.SessionResumeState, error) {
	return control.SessionResumeState{}, nil
}

func (c *orderedShutdownController) PrepareRuntimeShutdown() {
	if c.ctx.Err() == nil {
		c.contextStillAlive.Store(true)
	}
	c.prepareOnce.Do(func() { close(c.prepared) })
}

type orderedShutdownFactory struct {
	mu         sync.Mutex
	controller *orderedShutdownController
}

func (f *orderedShutdownFactory) CreateController(ctx context.Context, _ protocol.RuntimeTarget, sink event.Sink) (control.SessionAPI, error) {
	controller := &orderedShutdownController{
		fakeSessionController: newFakeSessionController(ctx, sink),
		prepared:              make(chan struct{}),
	}
	f.mu.Lock()
	f.controller = controller
	f.mu.Unlock()
	return controller, nil
}

func (f *orderedShutdownFactory) current() *orderedShutdownController {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.controller
}

// blockedAdmissionShutdownController is the smallest reproduction of the
// Controller/actor dependency at the durable admission boundary. The actor
// waits in the completion future while the Controller worker can finish only
// after PrepareRuntimeShutdown cancels it. Running preparation from the actor
// would therefore deadlock deterministically.
type blockedAdmissionShutdownController struct {
	*composerController

	completionEntered chan struct{}
	prepareEntered    chan struct{}
	admissionCancel   chan struct{}
	admissionResult   chan struct{}
	workerDone        chan struct{}

	completionOnce sync.Once
	prepareOnce    sync.Once
	cancelOnce     sync.Once
	contextAlive   atomic.Bool
}

func newBlockedAdmissionShutdownController(ctx context.Context, sink event.Sink) *blockedAdmissionShutdownController {
	return &blockedAdmissionShutdownController{
		composerController: &composerController{fakeSessionController: newFakeSessionController(ctx, sink)},
		completionEntered:  make(chan struct{}),
		prepareEntered:     make(chan struct{}),
		admissionCancel:    make(chan struct{}),
		admissionResult:    make(chan struct{}),
		workerDone:         make(chan struct{}),
	}
}

func (c *blockedAdmissionShutdownController) EnableDurableTurnAdmission() error { return nil }

func (c *blockedAdmissionShutdownController) ResumeWithRecovery(_ *agent.Session, _ string) (control.SessionResumeState, error) {
	return control.SessionResumeState{}, nil
}

func (c *blockedAdmissionShutdownController) PrepareDurableTurn(control.DurableTurnInput) (func() control.DurableTurnAdmissionResult, error) {
	return func() control.DurableTurnAdmissionResult {
		c.completionOnce.Do(func() { close(c.completionEntered) })
		<-c.admissionResult
		return control.DurableTurnAdmissionResult{Claimed: true, Err: context.Canceled}
	}, nil
}

func (c *blockedAdmissionShutdownController) SubmitDisplay(display, input string) {
	c.record("display", display, "", nil)
	c.mu.Lock()
	c.running = true
	c.mu.Unlock()
	c.sink.Emit(event.Event{Kind: event.TurnStarted})
	go func() {
		<-c.admissionCancel
		c.sink.Emit(event.Event{Kind: event.TurnDone, Err: context.Canceled})
		close(c.admissionResult)
		close(c.workerDone)
	}()
}

func (c *blockedAdmissionShutdownController) releaseAdmission() {
	c.cancelOnce.Do(func() { close(c.admissionCancel) })
}

func (c *blockedAdmissionShutdownController) PrepareRuntimeShutdown() {
	if c.ctx.Err() == nil {
		c.contextAlive.Store(true)
	}
	c.prepareOnce.Do(func() { close(c.prepareEntered) })
	c.releaseAdmission()
	<-c.workerDone
	c.mu.Lock()
	c.running = false
	c.mu.Unlock()
}

type blockedAdmissionShutdownFactory struct {
	mu         sync.Mutex
	controller *blockedAdmissionShutdownController
}

func (f *blockedAdmissionShutdownFactory) CreateController(ctx context.Context, _ protocol.RuntimeTarget, sink event.Sink) (control.SessionAPI, error) {
	ctx = context.WithValue(ctx, composerRootKey{}, "")
	controller := newBlockedAdmissionShutdownController(ctx, sink)
	f.mu.Lock()
	f.controller = controller
	f.mu.Unlock()
	return controller, nil
}

func (f *blockedAdmissionShutdownFactory) current() *blockedAdmissionShutdownController {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.controller
}

func TestRecoveryAwareFactorySeedsInterruptedRuntimeSnapshot(t *testing.T) {
	ids := &testIDSource{}
	delegate := &fakeControllerFactory{}
	factory := &recoveryAwareTestFactory{
		delegate: delegate,
		state:    control.SessionResumeState{PreviousTurnInterrupted: true},
	}
	manager, err := NewRuntimeManager(context.Background(), "host-restarted", factory, RuntimeManagerOptions{
		NewRuntimeEpoch: ids.runtimeEpoch, NewTurnID: ids.turnID,
		NewPromptID: ids.promptID, NewSubscriptionID: ids.subscriptionID,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	runtime, err := manager.GetOrCreate(testTarget())
	if err != nil {
		t.Fatal(err)
	}
	subscription, err := runtime.Subscribe(context.Background(), testAttachment(1), "")
	if err != nil {
		t.Fatal(err)
	}
	snapshot := subscription.Snapshot
	if snapshot.Running || snapshot.PendingPrompt != nil || snapshot.CurrentTurn != "" || snapshot.CancelRequested {
		t.Fatalf("cold recovery restored executable work: %+v", snapshot)
	}
	if snapshot.LastOutcome != protocol.OutcomeInterrupted || !snapshot.PreviousTurnInterrupted ||
		snapshot.InterruptionReason != protocol.InterruptionHostRestarted {
		t.Fatalf("cold recovery interruption = outcome=%q previous=%v reason=%q",
			snapshot.LastOutcome, snapshot.PreviousTurnInterrupted, snapshot.InterruptionReason)
	}
	factory.mu.Lock()
	recoveryCalls := factory.recoveryCalls
	factory.mu.Unlock()
	if recoveryCalls != 1 {
		t.Fatalf("recovery-aware factory calls = %d, want 1", recoveryCalls)
	}

	controller := delegate.controller(0)
	if _, err := runtime.Submit(context.Background(), "user explicitly continues"); err != nil {
		t.Fatal(err)
	}
	controller.releaseTurn()
	select {
	case <-controller.finished:
	case <-time.After(2 * time.Second):
		t.Fatal("continued turn did not finish")
	}
	after, err := runtime.Subscribe(context.Background(), testAttachment(1), subscription.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Snapshot.LastOutcome != protocol.OutcomeCompleted || after.Snapshot.PreviousTurnInterrupted || after.Snapshot.InterruptionReason != "" {
		t.Fatalf("completed continuation retained startup interruption: %+v", after.Snapshot)
	}
}

func TestRootCancellationOrdersRecoveryPreparationBeforeRuntimeContext(t *testing.T) {
	root, cancelRoot := context.WithCancel(context.Background())
	factory := &orderedShutdownFactory{}
	manager, err := NewRuntimeManager(root, "host-root-cancel", factory, RuntimeManagerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := manager.GetOrCreate(testTarget())
	if err != nil {
		t.Fatal(err)
	}
	controller := factory.current()
	if _, err := runtime.Submit(context.Background(), "active during root cancellation"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-controller.started:
	case <-time.After(2 * time.Second):
		t.Fatal("turn did not start")
	}

	cancelRoot()
	select {
	case <-controller.prepared:
	case <-time.After(2 * time.Second):
		t.Fatal("root cancellation bypassed recovery preparation")
	}
	select {
	case <-runtime.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("runtime did not close after ordered root cancellation")
	}
	if !controller.contextStillAlive.Load() {
		t.Fatal("runtime context was cancelled before PrepareRuntimeShutdown")
	}
	if _, err := manager.GetOrCreate(testTarget()); err != ErrRuntimeManagerClosed {
		t.Fatalf("GetOrCreate after root cancellation = %v, want ErrRuntimeManagerClosed", err)
	}
}

func TestRuntimeManagerCloseCancelsBlockedDurableAdmissionOutsideActor(t *testing.T) {
	factory := &blockedAdmissionShutdownFactory{}
	manager, err := NewRuntimeManager(context.Background(), "host-test", factory, RuntimeManagerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	runtime, err := manager.GetOrCreate(testTarget())
	if err != nil {
		t.Fatal(err)
	}
	controller := factory.current()
	// If a future regression fails before shutdown reaches Controller
	// preparation, release the deterministic fake so test cleanup cannot hang.
	defer controller.releaseAdmission()

	registry, err := idempotency.New("host-test", idempotency.Options{})
	if err != nil {
		t.Fatal(err)
	}
	type submitOutcome struct {
		result protocol.SessionSubmitResult
		err    error
	}
	submitDone := make(chan submitOutcome, 1)
	go func() {
		result, submitErr := runtime.ComposerSubmitMutation(
			context.Background(), registry,
			composerParams(runtime, "shutdown-blocked-admission", "persist before provider"), nil,
		)
		submitDone <- submitOutcome{result: result, err: submitErr}
	}()
	select {
	case <-controller.completionEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("composer actor did not block on durable admission completion")
	}

	closeDone := make(chan struct{})
	go func() {
		manager.Close()
		close(closeDone)
	}()
	select {
	case <-controller.prepareEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("RuntimeManager.Close did not prepare Controller outside the blocked actor")
	}
	if _, err := runtime.Snapshot(context.Background()); !errors.Is(err, ErrRuntimeClosed) {
		t.Fatalf("new actor admission during shutdown = %v, want ErrRuntimeClosed", err)
	}
	select {
	case <-controller.admissionCancel:
	case <-time.After(2 * time.Second):
		t.Fatal("Controller durable admission worker was not cancelled")
	}
	select {
	case <-closeDone:
	case <-time.After(2 * time.Second):
		t.Fatal("RuntimeManager.Close deadlocked behind durable admission")
	}
	select {
	case submitted := <-submitDone:
		// Prepare already crossed the authoritative transcript boundary. Shutdown
		// may prevent the guarded body from starting, but it cannot turn the same
		// semantic request into a retryable rejection/ghost user; Host caches the
		// TurnID and terminates it through the failed TurnDone invariant path.
		if submitted.err != nil || submitted.result.Kind != protocol.SubmitTurn || submitted.result.TurnID == "" {
			t.Fatalf("post-commit shutdown admission = %+v, %v", submitted.result, submitted.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled durable admission caller did not return")
	}
	if !controller.contextAlive.Load() {
		t.Fatal("runtime context was cancelled before Controller shutdown preparation")
	}
	if closeCalls, _ := controller.counts(); closeCalls != 1 {
		t.Fatalf("Controller Close calls = %d, want 1", closeCalls)
	}
}

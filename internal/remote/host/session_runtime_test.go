package host

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/checkpoint"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/eventwire"
	"reasonix/internal/evidence"
	"reasonix/internal/jobs"
	"reasonix/internal/provider"
	"reasonix/internal/remote/idempotency"
	"reasonix/internal/remote/protocol"
)

type testIDSource struct{ next atomic.Uint64 }

func (s *testIDSource) runtimeEpoch() (protocol.RuntimeEpoch, error) {
	return protocol.RuntimeEpoch(fmt.Sprintf("runtime-%d", s.next.Add(1))), nil
}

func (s *testIDSource) turnID() (protocol.TurnID, error) {
	return protocol.TurnID(fmt.Sprintf("turn-%d", s.next.Add(1))), nil
}

func (s *testIDSource) operationID() (protocol.OperationID, error) {
	return protocol.OperationID(fmt.Sprintf("operation-%d", s.next.Add(1))), nil
}

func (s *testIDSource) promptID() (protocol.PromptID, error) {
	return protocol.PromptID(fmt.Sprintf("prompt-%d", s.next.Add(1))), nil
}

func (s *testIDSource) subscriptionID() (protocol.SubscriptionID, error) {
	return protocol.SubscriptionID(fmt.Sprintf("subscription-%d", s.next.Add(1))), nil
}

type fakeControllerFactory struct {
	mu          sync.Mutex
	controllers []*fakeSessionController
}

func (f *fakeControllerFactory) CreateController(ctx context.Context, _ protocol.RuntimeTarget, sink event.Sink) (control.SessionAPI, error) {
	controller := newFakeSessionController(ctx, sink)
	f.mu.Lock()
	f.controllers = append(f.controllers, controller)
	f.mu.Unlock()
	return controller, nil
}

func (f *fakeControllerFactory) controller(index int) *fakeSessionController {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.controllers[index]
}

type fakeSessionController struct {
	control.SessionAPI

	ctx  context.Context
	sink event.Sink

	mu              sync.Mutex
	running         bool
	cancelRequested bool
	closeCalls      int
	snapshotCalls   int
	snapshotErr     error
	tryCancelCalls  int
	strictSubmits   int
	strictInputs    []string
	strictDisplays  []string
	release         chan struct{}
	releaseOnce     sync.Once
	started         chan string
	finished        chan struct{}
	finishOnce      sync.Once
	runEvent        event.Event
	steers          []string
	trySteerCalls   int
	approvals       []fakeApprovalCall
	answers         []fakeAnswerCall
	steerAccepted   bool
	steerEntered    chan struct{}
	steerRelease    chan struct{}
	approveEntered  chan struct{}
	approveRelease  chan struct{}
	answerEntered   chan struct{}
	answerRelease   chan struct{}
	operationCore   *control.Controller
	operationSpecs  []control.OperationSpec
	operationMap    func(control.OperationSpec) control.OperationSpec
	checkpointState control.CheckpointSnapshot
}

type fakeApprovalCall struct {
	ID      string
	Allow   bool
	Session bool
	Persist bool
}

type fakeAnswerCall struct {
	ID      string
	Answers []event.AskAnswer
}

func newFakeSessionController(ctx context.Context, sink event.Sink) *fakeSessionController {
	controller := &fakeSessionController{
		ctx: ctx, sink: sink, release: make(chan struct{}), started: make(chan string, 1),
		finished:      make(chan struct{}),
		steerAccepted: true,
		runEvent: event.Event{Kind: event.ToolResult, Tool: event.Tool{
			ID: "tool-1", Name: "bash", Args: `{"command":"printf remote"}`,
			Output: "remote output", FileDiff: event.FileDiff{Diff: "@@ -1 +1 @@", Added: 1, Removed: 1},
			Profile: &event.Profile{Model: "deepseek-test", Effort: "high"},
		}},
	}
	controller.operationCore = control.New(control.Options{Sink: sink})
	controller.checkpointState = control.CheckpointSnapshot{
		Metas: []checkpoint.Meta{}, TurnsByMessageIndex: map[int]int{}, ConversationAvailable: map[int]bool{},
	}
	return controller
}

func (c *fakeSessionController) WorkspaceRoot() string { return "" }
func (c *fakeSessionController) SessionPath() string   { return "" }
func (c *fakeSessionController) SessionDir() string    { return "" }
func (c *fakeSessionController) Turn() int             { return 0 }
func (c *fakeSessionController) History() []provider.Message {
	return []provider.Message{}
}
func (c *fakeSessionController) Todos() []evidence.TodoItem  { return []evidence.TodoItem{} }
func (c *fakeSessionController) ContextSnapshot() (int, int) { return 0, 0 }
func (c *fakeSessionController) LastUsage() *provider.Usage  { return nil }
func (c *fakeSessionController) Jobs() []jobs.View           { return []jobs.View{} }
func (c *fakeSessionController) CheckpointSnapshot() control.CheckpointSnapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	return control.CheckpointSnapshot{
		Metas:                 append([]checkpoint.Meta(nil), c.checkpointState.Metas...),
		TurnsByMessageIndex:   cloneTestIntMap(c.checkpointState.TurnsByMessageIndex),
		ConversationAvailable: cloneTestBoolMap(c.checkpointState.ConversationAvailable),
	}
}

func cloneTestIntMap(in map[int]int) map[int]int {
	out := make(map[int]int, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func cloneTestBoolMap(in map[int]bool) map[int]bool {
	out := make(map[int]bool, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func (c *fakeSessionController) Submit(input string) {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		panic("fake controller received overlapping Submit")
	}
	c.running = true
	c.cancelRequested = false
	c.mu.Unlock()
	c.sink.Emit(event.Event{Kind: event.TurnStarted})
	select {
	case c.started <- input:
	default:
	}
	go func() {
		select {
		case <-c.release:
			c.sink.Emit(c.runEvent)
			c.mu.Lock()
			c.running = false
			c.mu.Unlock()
			c.sink.Emit(event.Event{Kind: event.TurnDone})
		case <-c.ctx.Done():
		}
		c.finishOnce.Do(func() { close(c.finished) })
	}()
}

func (c *fakeSessionController) SubmitUserTurn(input, display string) {
	c.mu.Lock()
	c.strictSubmits++
	c.strictInputs = append(c.strictInputs, input)
	c.strictDisplays = append(c.strictDisplays, display)
	c.mu.Unlock()
	c.Submit(input)
}

func (c *fakeSessionController) TryCancel() control.CancelAttempt {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.tryCancelCalls++
	if !c.running {
		return control.CancelNotActive
	}
	if c.cancelRequested {
		return control.CancelAlreadyRequested
	}
	c.cancelRequested = true
	return control.CancelRequestedNow
}

func (c *fakeSessionController) StartOperation(spec control.OperationSpec) (*control.OperationHandle, error) {
	c.mu.Lock()
	c.operationSpecs = append(c.operationSpecs, spec)
	mapped := spec
	if c.operationMap != nil {
		mapped = c.operationMap(spec)
	}
	core := c.operationCore
	c.mu.Unlock()
	return core.StartOperation(mapped)
}

func (c *fakeSessionController) TrySteer(text string) bool {
	c.mu.Lock()
	c.trySteerCalls++
	accepted := c.running && c.steerAccepted
	if accepted {
		c.steers = append(c.steers, text)
	}
	entered, release := c.steerEntered, c.steerRelease
	c.mu.Unlock()
	if !accepted {
		return false
	}
	notifyFakeCall(entered, release)
	return true
}

func (c *fakeSessionController) Approve(id string, allow, session, persist bool) {
	c.mu.Lock()
	c.approvals = append(c.approvals, fakeApprovalCall{ID: id, Allow: allow, Session: session, Persist: persist})
	entered, release := c.approveEntered, c.approveRelease
	c.mu.Unlock()
	notifyFakeCall(entered, release)
}

func (c *fakeSessionController) AnswerQuestion(id string, answers []event.AskAnswer) {
	copyAnswers := make([]event.AskAnswer, len(answers))
	for index, answer := range answers {
		copyAnswers[index] = event.AskAnswer{QuestionID: answer.QuestionID, Selected: append([]string(nil), answer.Selected...)}
	}
	c.mu.Lock()
	c.answers = append(c.answers, fakeAnswerCall{ID: id, Answers: copyAnswers})
	entered, release := c.answerEntered, c.answerRelease
	c.mu.Unlock()
	notifyFakeCall(entered, release)
}

func notifyFakeCall(entered, release chan struct{}) {
	if entered != nil {
		select {
		case entered <- struct{}{}:
		default:
		}
	}
	if release != nil {
		<-release
	}
}

func (c *fakeSessionController) Running() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.running
}

func (c *fakeSessionController) CancelRequested() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cancelRequested
}

func (c *fakeSessionController) Close() {
	c.mu.Lock()
	c.closeCalls++
	c.running = false
	core := c.operationCore
	c.mu.Unlock()
	if core != nil {
		core.Close()
	}
}

func (c *fakeSessionController) Snapshot() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.snapshotCalls++
	return c.snapshotErr
}

func (c *fakeSessionController) releaseTurn() {
	c.releaseOnce.Do(func() { close(c.release) })
}

func (c *fakeSessionController) emit(value event.Event) { c.sink.Emit(value) }

func (c *fakeSessionController) counts() (closeCalls, tryCancelCalls int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closeCalls, c.tryCancelCalls
}

func (c *fakeSessionController) strictSubmitCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.strictSubmits
}

func (c *fakeSessionController) strictSubmitArguments() ([]string, []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.strictInputs...), append([]string(nil), c.strictDisplays...)
}

func (c *fakeSessionController) operationSpecSnapshot() []control.OperationSpec {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]control.OperationSpec(nil), c.operationSpecs...)
}

func (c *fakeSessionController) promptMutationCalls() ([]string, []fakeApprovalCall, []fakeAnswerCall) {
	c.mu.Lock()
	defer c.mu.Unlock()
	steers := append([]string(nil), c.steers...)
	approvals := append([]fakeApprovalCall(nil), c.approvals...)
	answers := make([]fakeAnswerCall, len(c.answers))
	for index, answer := range c.answers {
		answers[index] = fakeAnswerCall{ID: answer.ID, Answers: make([]event.AskAnswer, len(answer.Answers))}
		for answerIndex, value := range answer.Answers {
			answers[index].Answers[answerIndex] = event.AskAnswer{
				QuestionID: value.QuestionID, Selected: append([]string(nil), value.Selected...),
			}
		}
	}
	return steers, approvals, answers
}

func newTestRuntimeManager(t *testing.T, root context.Context, queue, logLimit int) (*RuntimeManager, *fakeControllerFactory) {
	t.Helper()
	ids := &testIDSource{}
	factory := &fakeControllerFactory{}
	manager, err := NewRuntimeManager(root, "host-test", factory, RuntimeManagerOptions{
		NewRuntimeEpoch: ids.runtimeEpoch, NewTurnID: ids.turnID, NewOperationID: ids.operationID, NewPromptID: ids.promptID,
		NewSubscriptionID: ids.subscriptionID,
		SubscriptionQueue: queue, EventLogLimit: logLimit,
	})
	if err != nil {
		t.Fatalf("NewRuntimeManager: %v", err)
	}
	t.Cleanup(manager.Close)
	return manager, factory
}

func testTarget() protocol.RuntimeTarget {
	return protocol.RuntimeTarget{WorkspaceID: "workspace-test", SessionID: "session-test"}
}

func testAttachment(generation uint64) AttachmentKey {
	return AttachmentForLease(LeaseBinding{
		ClientInstanceID: "client-test", LeaseID: "lease-test", Generation: generation,
	})
}

func receiveMessage(t *testing.T, messages <-chan SubscriptionMessage) SubscriptionMessage {
	t.Helper()
	select {
	case message, ok := <-messages:
		if !ok {
			t.Fatal("subscription closed before expected message")
		}
		return message
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for subscription message")
		return SubscriptionMessage{}
	}
}

func requireClosed(t *testing.T, messages <-chan SubscriptionMessage) {
	t.Helper()
	select {
	case _, ok := <-messages:
		if ok {
			t.Fatal("subscription still delivered a buffered message after detach")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for subscription close")
	}
}

func TestAttachDetachDoesNotOwnTurnAndReconnectRecovers(t *testing.T) {
	root, cancelRoot := context.WithCancel(context.Background())
	defer cancelRoot()
	manager, factory := newTestRuntimeManager(t, root, 16, 64)
	runtime, err := manager.GetOrCreate(testTarget())
	if err != nil {
		t.Fatal(err)
	}
	controller := factory.controller(0)
	firstAttachment := testAttachment(1)
	first, err := runtime.Subscribe(context.Background(), firstAttachment, "")
	if err != nil {
		t.Fatal(err)
	}
	if first.Snapshot.BoundarySeq != 0 {
		t.Fatalf("initial boundary = %d, want 0", first.Snapshot.BoundarySeq)
	}

	submitted, err := runtime.Submit(context.Background(), "continue after SSH EOF")
	if err != nil {
		t.Fatal(err)
	}
	started := receiveMessage(t, first.Messages)
	if started.Event == nil || started.Event.Event.Kind != "turn_started" || started.Event.TurnID != submitted.TurnID {
		t.Fatalf("turn_started envelope = %+v", started)
	}

	if err := manager.DetachAttachment(firstAttachment); err != nil {
		t.Fatal(err)
	}
	requireClosed(t, first.Messages)
	closeCalls, tryCancelCalls := controller.counts()
	if closeCalls != 0 || tryCancelCalls != 0 {
		t.Fatalf("detach called Controller lifecycle methods: close=%d tryCancel=%d", closeCalls, tryCancelCalls)
	}
	select {
	case <-controller.ctx.Done():
		t.Fatal("attach detach cancelled daemon-owned Controller context")
	default:
	}

	controller.releaseTurn()
	select {
	case <-controller.finished:
	case <-time.After(2 * time.Second):
		t.Fatal("accepted turn did not continue after detach")
	}

	second, err := runtime.Subscribe(context.Background(), testAttachment(2), "")
	if err != nil {
		t.Fatal(err)
	}
	snapshot := second.Snapshot
	if snapshot.RuntimeEpoch != submitted.RuntimeEpoch || snapshot.Running || snapshot.CurrentTurn != "" {
		t.Fatalf("reconnect runtime state = %+v", snapshot)
	}
	if snapshot.LastOutcome != protocol.OutcomeCompleted || snapshot.BoundarySeq != 3 {
		t.Fatalf("reconnect outcome/boundary = %q/%d", snapshot.LastOutcome, snapshot.BoundarySeq)
	}
	if len(snapshot.Events) != 0 {
		t.Fatalf("completed Turn retained %d live semantic events; canonical history owns completed content", len(snapshot.Events))
	}

	manager.Close()
	closeCalls, _ = controller.counts()
	if closeCalls != 1 {
		t.Fatalf("daemon Close calls = %d, want 1", closeCalls)
	}
	select {
	case <-controller.ctx.Done():
	default:
		t.Fatal("daemon Close did not cancel Controller context")
	}
}

func TestCancelTurnRequiresExactOpaqueIDAndUsesStrictPrimitive(t *testing.T) {
	manager, factory := newTestRuntimeManager(t, context.Background(), 16, 64)
	runtime, err := manager.GetOrCreate(testTarget())
	if err != nil {
		t.Fatal(err)
	}
	controller := factory.controller(0)
	submitted, err := runtime.Submit(context.Background(), "cancel me exactly")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := runtime.CancelTurn(context.Background(), "turn-forged"); !errors.Is(err, ErrTurnMismatch) {
		t.Fatalf("wrong turn cancel error = %v", err)
	}
	_, tryCancelCalls := controller.counts()
	if tryCancelCalls != 0 {
		t.Fatalf("mismatched ID reached Controller.TryCancel %d times", tryCancelCalls)
	}

	first, err := runtime.CancelTurn(context.Background(), submitted.TurnID)
	if err != nil || first.Status != protocol.CancelRequested {
		t.Fatalf("first exact cancel = %+v, %v", first, err)
	}
	second, err := runtime.CancelTurn(context.Background(), submitted.TurnID)
	if err != nil || second.Status != protocol.CancelAlreadyRequested {
		t.Fatalf("repeated exact cancel = %+v, %v", second, err)
	}
	_, tryCancelCalls = controller.counts()
	if tryCancelCalls != 2 {
		t.Fatalf("strict TryCancel calls = %d, want 2", tryCancelCalls)
	}

	controller.releaseTurn()
	select {
	case <-controller.finished:
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled fake turn did not finish")
	}
	snapshot, err := runtime.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.LastOutcome != protocol.OutcomeCancelled || snapshot.Running || snapshot.CancelRequested {
		t.Fatalf("cancelled snapshot = %+v", snapshot)
	}
	if _, err := runtime.CancelTurn(context.Background(), submitted.TurnID); !errors.Is(err, ErrTurnNotActive) {
		t.Fatalf("late cancel error = %v", err)
	}
}

func TestSessionMutationIdempotencyIsActorAtomicAndReplaysFirstAdmission(t *testing.T) {
	manager, factory := newTestRuntimeManager(t, context.Background(), 16, 64)
	runtime, err := manager.GetOrCreate(testTarget())
	if err != nil {
		t.Fatal(err)
	}
	registry, err := idempotency.New("host-test", idempotency.Options{})
	if err != nil {
		t.Fatal(err)
	}
	params := protocol.SessionSubmitParams{
		SessionMutation: protocol.SessionMutation{
			RequestID: "request-submit", ExpectedHostEpoch: "host-test", Target: testTarget(),
			ExpectedRuntimeEpoch: runtime.Epoch(),
		},
		Input: "expanded model input", DisplayText: "visible composer text",
	}
	first, err := runtime.SubmitMutation(context.Background(), registry, params, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := runtime.SubmitMutation(context.Background(), registry, params, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if replay != first || factory.controller(0).strictSubmitCount() != 1 {
		t.Fatalf("submit replay=%+v first=%+v Controller calls=%d", replay, first, factory.controller(0).strictSubmitCount())
	}
	inputs, displays := factory.controller(0).strictSubmitArguments()
	if !reflect.DeepEqual(inputs, []string{"expanded model input"}) || !reflect.DeepEqual(displays, []string{"visible composer text"}) {
		t.Fatalf("SubmitUserTurn arguments = input %q display %q", inputs, displays)
	}
	changed := params
	changed.Input = "different operation"
	changed.DisplayText = changed.Input
	if _, err := runtime.SubmitMutation(context.Background(), registry, changed, nil, nil); err == nil {
		t.Fatal("conflicting requestId reuse was accepted")
	} else {
		var remote *protocol.RemoteError
		if !errors.As(err, &remote) || remote.Code != protocol.ErrRequestIDConflict {
			t.Fatalf("conflicting reuse error = %v", err)
		}
	}

	// A fresh mutation is registered before state admission. Its deterministic
	// busy rejection is cached and remains the first answer after the turn ends.
	busy := params
	busy.RequestID = "request-busy"
	if _, err := runtime.SubmitMutation(context.Background(), registry, busy, nil, nil); err == nil {
		t.Fatal("overlapping submit was accepted")
	} else {
		var remote *protocol.RemoteError
		if !errors.As(err, &remote) || remote.Code != protocol.ErrTurnAlreadyRunning {
			t.Fatalf("busy error = %v", err)
		}
	}
	factory.controller(0).releaseTurn()
	select {
	case <-factory.controller(0).finished:
	case <-time.After(2 * time.Second):
		t.Fatal("turn did not finish")
	}
	if _, err := runtime.Snapshot(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.SubmitMutation(context.Background(), registry, busy, nil, nil); err == nil {
		t.Fatal("cached busy rejection changed after runtime state changed")
	} else {
		var remote *protocol.RemoteError
		if !errors.As(err, &remote) || remote.Code != protocol.ErrTurnAlreadyRunning {
			t.Fatalf("busy replay error = %v", err)
		}
	}
	if factory.controller(0).strictSubmitCount() != 1 {
		t.Fatalf("business rejection replay executed Controller %d times", factory.controller(0).strictSubmitCount())
	}
}

func TestSessionMutationStaleEpochIsUncachedAndCancelReplaySurvivesTurnDone(t *testing.T) {
	manager, factory := newTestRuntimeManager(t, context.Background(), 16, 64)
	runtime, err := manager.GetOrCreate(testTarget())
	if err != nil {
		t.Fatal(err)
	}
	registry, err := idempotency.New("host-test", idempotency.Options{})
	if err != nil {
		t.Fatal(err)
	}
	params := protocol.SessionSubmitParams{
		SessionMutation: protocol.SessionMutation{
			RequestID: "request-stale", ExpectedHostEpoch: "host-test", Target: testTarget(),
			ExpectedRuntimeEpoch: "runtime-stale",
		},
		Input: "retry after stale", DisplayText: "retry after stale",
	}
	if _, err := runtime.SubmitMutation(context.Background(), registry, params, nil, nil); err == nil {
		t.Fatal("stale runtime epoch was accepted")
	} else {
		var remote *protocol.RemoteError
		if !errors.As(err, &remote) || remote.Code != protocol.ErrStaleRuntimeEpoch {
			t.Fatalf("stale error = %v", err)
		}
	}
	if stats := registry.Stats(); stats.Entries != 0 {
		t.Fatalf("stale epoch entered idempotency cache: %+v", stats)
	}
	params.ExpectedRuntimeEpoch = runtime.Epoch()
	submitted, err := runtime.SubmitMutation(context.Background(), registry, params, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	cancel := protocol.TurnCancelParams{
		SessionMutation: protocol.SessionMutation{
			RequestID: "request-cancel", ExpectedHostEpoch: "host-test", Target: testTarget(),
			ExpectedRuntimeEpoch: runtime.Epoch(),
		},
		ExpectedTurnID: submitted.TurnID,
	}
	firstCancel, err := runtime.CancelTurnMutation(context.Background(), registry, cancel, nil)
	if err != nil || firstCancel.Status != protocol.CancelRequested {
		t.Fatalf("first cancel = %+v, %v", firstCancel, err)
	}
	factory.controller(0).releaseTurn()
	select {
	case <-factory.controller(0).finished:
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled turn did not finish")
	}
	if _, err := runtime.Snapshot(context.Background()); err != nil {
		t.Fatal(err)
	}
	replay, err := runtime.CancelTurnMutation(context.Background(), registry, cancel, nil)
	if err != nil || replay != firstCancel {
		t.Fatalf("late cancel replay = %+v, %v; want %+v", replay, err, firstCancel)
	}
	_, cancelCalls := factory.controller(0).counts()
	if cancelCalls != 1 {
		t.Fatalf("cancel replay reached Controller.TryCancel %d times", cancelCalls)
	}
}

func TestQueuedSubmitAndCancelRejectResumedLeaseGenerationBeforeIdempotencyBegin(t *testing.T) {
	manager, factory := newTestRuntimeManager(t, context.Background(), 16, 64)
	runtime, err := manager.GetOrCreate(testTarget())
	if err != nil {
		t.Fatal(err)
	}
	registry, err := idempotency.New("host-test", idempotency.Options{})
	if err != nil {
		t.Fatal(err)
	}
	clock := &leaseTestClock{now: time.Unix(5_000, 0)}
	leases := newLeaseTestManager(clock)
	first, err := leases.Acquire("client-generation", "")
	if err != nil {
		t.Fatal(err)
	}
	params := protocol.SessionSubmitParams{
		SessionMutation: protocol.SessionMutation{
			RequestID: "request-generation-submit", ExpectedHostEpoch: "host-test",
			Target: testTarget(), ExpectedRuntimeEpoch: runtime.Epoch(),
		},
		Input: "admit only from the resumed transport", DisplayText: "admit only from the resumed transport",
	}

	unblockSubmit := blockRuntimeActor(t, runtime)
	type submitOutcome struct {
		result protocol.SessionSubmitResult
		err    error
	}
	submitDone := make(chan submitOutcome, 1)
	go func() {
		result, callErr := runtime.SubmitMutation(context.Background(), registry, params, nil, func() error {
			return leases.Validate(first.Binding, false)
		})
		submitDone <- submitOutcome{result: result, err: callErr}
	}()
	waitRuntimeMailboxQueued(t, runtime)
	second, err := leases.Acquire("client-generation", first.Binding.LeaseID)
	if err != nil {
		t.Fatal(err)
	}
	unblockSubmit()
	select {
	case outcome := <-submitDone:
		requireRemoteCode(t, outcome.err, protocol.ErrStaleConnection)
		if outcome.result != (protocol.SessionSubmitResult{}) {
			t.Fatalf("stale-generation Submit result = %+v", outcome.result)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("queued old-generation Submit did not finish")
	}
	if stats := registry.Stats(); stats.Entries != 0 || stats.Pending != 0 || stats.Completed != 0 {
		t.Fatalf("old-generation Submit created requestId state: %+v", stats)
	}
	controller := factory.controller(0)
	if controller.strictSubmitCount() != 0 {
		t.Fatalf("old-generation Submit reached Controller %d times", controller.strictSubmitCount())
	}
	submitted, err := runtime.SubmitMutation(context.Background(), registry, params, nil, func() error {
		return leases.Validate(second.Binding, false)
	})
	if err != nil || submitted.TurnID == "" {
		t.Fatalf("same requestId from resumed generation = %+v, %v", submitted, err)
	}
	if controller.strictSubmitCount() != 1 {
		t.Fatalf("resumed-generation Submit reached Controller %d times", controller.strictSubmitCount())
	}

	cancel := protocol.TurnCancelParams{
		SessionMutation: protocol.SessionMutation{
			RequestID: "request-generation-cancel", ExpectedHostEpoch: "host-test",
			Target: testTarget(), ExpectedRuntimeEpoch: runtime.Epoch(),
		},
		ExpectedTurnID: submitted.TurnID,
	}
	beforeCancel := registry.Stats()
	unblockCancel := blockRuntimeActor(t, runtime)
	type cancelOutcome struct {
		result protocol.TurnCancelResult
		err    error
	}
	cancelDone := make(chan cancelOutcome, 1)
	go func() {
		result, callErr := runtime.CancelTurnMutation(context.Background(), registry, cancel, func() error {
			return leases.Validate(second.Binding, false)
		})
		cancelDone <- cancelOutcome{result: result, err: callErr}
	}()
	waitRuntimeMailboxQueued(t, runtime)
	third, err := leases.Acquire("client-generation", second.Binding.LeaseID)
	if err != nil {
		t.Fatal(err)
	}
	unblockCancel()
	select {
	case outcome := <-cancelDone:
		requireRemoteCode(t, outcome.err, protocol.ErrStaleConnection)
		if outcome.result != (protocol.TurnCancelResult{}) {
			t.Fatalf("stale-generation Cancel result = %+v", outcome.result)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("queued old-generation Cancel did not finish")
	}
	if afterCancel := registry.Stats(); afterCancel != beforeCancel {
		t.Fatalf("old-generation Cancel changed requestId state: before=%+v after=%+v", beforeCancel, afterCancel)
	}
	if _, cancelCalls := controller.counts(); cancelCalls != 0 {
		t.Fatalf("old-generation Cancel reached Controller.TryCancel %d times", cancelCalls)
	}
	cancelled, err := runtime.CancelTurnMutation(context.Background(), registry, cancel, func() error {
		return leases.Validate(third.Binding, false)
	})
	if err != nil || cancelled.Status != protocol.CancelRequested || cancelled.TurnID != submitted.TurnID {
		t.Fatalf("same Cancel requestId from resumed generation = %+v, %v", cancelled, err)
	}
	if _, cancelCalls := controller.counts(); cancelCalls != 1 {
		t.Fatalf("resumed-generation Cancel reached Controller.TryCancel %d times", cancelCalls)
	}
	controller.releaseTurn()
}

func TestTurnIDsAreNeverReusedAndSubmitUsesNormalPromptPrimitive(t *testing.T) {
	factory := &fakeControllerFactory{}
	var turnCalls atomic.Int32
	manager, err := NewRuntimeManager(context.Background(), "host-test", factory, RuntimeManagerOptions{
		NewRuntimeEpoch: func() (protocol.RuntimeEpoch, error) { return "runtime-fixed", nil },
		NewTurnID: func() (protocol.TurnID, error) {
			switch turnCalls.Add(1) {
			case 1, 2:
				return "turn-issued-once", nil
			default:
				return "turn-new", nil
			}
		},
		NewSubscriptionID: func() (protocol.SubscriptionID, error) { return "subscription-fixed", nil },
		SubscriptionQueue: 8,
		EventLogLimit:     32,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	runtime, err := manager.GetOrCreate(testTarget())
	if err != nil {
		t.Fatal(err)
	}
	controller := factory.controller(0)

	first, err := runtime.Submit(context.Background(), "/new is literal prompt text in the Stage 2 turn primitive")
	if err != nil {
		t.Fatal(err)
	}
	if first.TurnID != "turn-issued-once" || controller.strictSubmitCount() != 1 {
		t.Fatalf("first strict submit = %+v, calls=%d", first, controller.strictSubmitCount())
	}
	controller.releaseTurn()
	select {
	case <-controller.finished:
	case <-time.After(2 * time.Second):
		t.Fatal("first turn did not finish")
	}
	if _, err := runtime.Snapshot(context.Background()); err != nil {
		t.Fatal(err)
	}

	second, err := runtime.Submit(context.Background(), "second prompt")
	if err != nil {
		t.Fatal(err)
	}
	if second.TurnID != "turn-new" || second.TurnID == first.TurnID {
		t.Fatalf("reused issued turn ID: first=%q second=%q", first.TurnID, second.TurnID)
	}
	if got := turnCalls.Load(); got != 3 {
		t.Fatalf("turn generator calls = %d, want duplicate retry", got)
	}
	if controller.strictSubmitCount() != 2 {
		t.Fatalf("strict SubmitUserTurn calls = %d, want 2", controller.strictSubmitCount())
	}
}

func TestSubscriptionIDsAreNeverReusedAfterUnsubscribe(t *testing.T) {
	factory := &fakeControllerFactory{}
	var subscriptionCalls atomic.Int32
	manager, err := NewRuntimeManager(context.Background(), "host-test", factory, RuntimeManagerOptions{
		NewRuntimeEpoch: func() (protocol.RuntimeEpoch, error) { return "runtime-fixed", nil },
		NewTurnID:       func() (protocol.TurnID, error) { return "turn-fixed", nil },
		NewSubscriptionID: func() (protocol.SubscriptionID, error) {
			switch subscriptionCalls.Add(1) {
			case 1, 2:
				return "subscription-issued-once", nil
			default:
				return "subscription-new", nil
			}
		},
		SubscriptionQueue: 8,
		EventLogLimit:     32,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	runtime, err := manager.GetOrCreate(testTarget())
	if err != nil {
		t.Fatal(err)
	}
	attachment := testAttachment(1)
	first, err := runtime.Subscribe(context.Background(), attachment, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.Unsubscribe(context.Background(), attachment, first.ID); err != nil {
		t.Fatal(err)
	}
	requireClosed(t, first.Messages)

	second, err := runtime.Subscribe(context.Background(), attachment, "")
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == second.ID || second.ID != "subscription-new" {
		t.Fatalf("subscription ID reused: first=%q second=%q", first.ID, second.ID)
	}
	if got := subscriptionCalls.Load(); got != 3 {
		t.Fatalf("subscription generator calls = %d, want collision retry", got)
	}
	// A delayed idempotent unsubscribe for the old ID must not delete the new
	// subscription on the same attachment.
	if err := runtime.Unsubscribe(context.Background(), attachment, first.ID); err != nil {
		t.Fatal(err)
	}
	factory.controller(0).emit(event.Event{Kind: event.Notice, Text: "new-subscription-alive"})
	if _, err := runtime.Snapshot(context.Background()); err != nil {
		t.Fatal(err)
	}
	if message := receiveMessage(t, second.Messages); message.Event == nil || message.Event.Event.Text != "new-subscription-alive" {
		t.Fatalf("new subscription was affected by delayed unsubscribe: %+v", message)
	}
}

func TestSubscriptionIDsAreDaemonGlobalAcrossCurrentRuntimes(t *testing.T) {
	factory := &fakeControllerFactory{}
	ids := &testIDSource{}
	var calls atomic.Int32
	manager, err := NewRuntimeManager(context.Background(), "host-test", factory, RuntimeManagerOptions{
		NewRuntimeEpoch: ids.runtimeEpoch,
		NewTurnID:       ids.turnID,
		NewSubscriptionID: func() (protocol.SubscriptionID, error) {
			switch calls.Add(1) {
			case 1, 2:
				return "subscription-global", nil
			default:
				return "subscription-second", nil
			}
		},
		SubscriptionQueue: 8,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	first, err := manager.GetOrCreate(testTarget())
	if err != nil {
		t.Fatal(err)
	}
	secondTarget := protocol.RuntimeTarget{WorkspaceID: "workspace-test", SessionID: "session-second"}
	second, err := manager.GetOrCreate(secondTarget)
	if err != nil {
		t.Fatal(err)
	}
	firstSubscription, err := first.Subscribe(context.Background(), testAttachment(1), "")
	if err != nil {
		t.Fatal(err)
	}
	secondSubscription, err := second.Subscribe(context.Background(), testAttachment(1), "")
	if err != nil {
		t.Fatal(err)
	}
	if firstSubscription.ID != "subscription-global" || secondSubscription.ID != "subscription-second" || firstSubscription.ID == secondSubscription.ID {
		t.Fatalf("global subscription IDs = first %q second %q", firstSubscription.ID, secondSubscription.ID)
	}
	if got := calls.Load(); got != 3 {
		t.Fatalf("subscription generator calls = %d, want cross-runtime collision retry", got)
	}
}

func TestSubscriptionIDsAreDaemonGlobalAcrossRetiredAndCurrentRuntimes(t *testing.T) {
	factory := &fakeControllerFactory{}
	ids := &testIDSource{}
	var calls atomic.Int32
	manager, err := NewRuntimeManager(context.Background(), "host-test", factory, RuntimeManagerOptions{
		NewRuntimeEpoch: ids.runtimeEpoch,
		NewTurnID:       ids.turnID,
		NewSubscriptionID: func() (protocol.SubscriptionID, error) {
			switch calls.Add(1) {
			case 1, 2:
				return "subscription-retired", nil
			default:
				return "subscription-current", nil
			}
		},
		SubscriptionQueue: 8,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	oldRuntime, err := manager.GetOrCreate(testTarget())
	if err != nil {
		t.Fatal(err)
	}
	oldSubscription, err := oldRuntime.Subscribe(context.Background(), testAttachment(1), "")
	if err != nil {
		t.Fatal(err)
	}
	replacement, err := manager.Replace(testTarget())
	if err != nil {
		t.Fatal(err)
	}
	terminal := receiveMessage(t, oldSubscription.Messages)
	if terminal.Resync == nil || terminal.Resync.Reason != protocol.ResyncRuntimeReplaced {
		t.Fatalf("retired terminal = %+v", terminal)
	}
	install, err := manager.Subscribe(context.Background(), testTarget(), testAttachment(1), "")
	if err != nil {
		t.Fatal(err)
	}
	if install.Runtime != replacement || install.Subscription.ID != "subscription-current" || install.Subscription.ID == oldSubscription.ID {
		t.Fatalf("retired/current IDs = old %q new %q", oldSubscription.ID, install.Subscription.ID)
	}
	if got := calls.Load(); got != 3 {
		t.Fatalf("subscription generator calls = %d, want retired collision retry", got)
	}
	if err := install.Commit(); err != nil {
		t.Fatal(err)
	}
	select {
	case message := <-oldSubscription.Messages:
		t.Fatalf("unrelated current subscribe consumed retired terminal identity: %+v", message)
	case <-time.After(25 * time.Millisecond):
	}
}

func TestManagedSameRuntimeSubscriptionReplacementCommitAbort(t *testing.T) {
	manager, factory := newTestRuntimeManager(t, context.Background(), 16, 64)
	attachment := testAttachment(1)
	initial, err := manager.Subscribe(context.Background(), testTarget(), attachment, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := initial.Commit(); err != nil {
		t.Fatal(err)
	}

	firstAttempt, err := manager.Subscribe(context.Background(), testTarget(), attachment, initial.Subscription.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Subscribe(context.Background(), testTarget(), attachment, initial.Subscription.ID); !errors.Is(err, ErrSubscriptionNotFound) {
		t.Fatalf("concurrent replacement error = %v", err)
	}
	factory.controller(0).emit(event.Event{Kind: event.Notice, Text: "old-remains-live-during-projection"})
	if _, err := initial.Runtime.Snapshot(context.Background()); err != nil {
		t.Fatal(err)
	}
	if message := receiveMessage(t, initial.Subscription.Messages); message.Event == nil || message.Event.Event.Text != "old-remains-live-during-projection" {
		t.Fatalf("old subscription stopped before Commit: %+v", message)
	}
	if message := receiveMessage(t, firstAttempt.Subscription.Messages); message.Event == nil || message.Event.Event.Text != "old-remains-live-during-projection" {
		t.Fatalf("new provisional subscription missed live event: %+v", message)
	}
	if err := firstAttempt.Abort(); err != nil {
		t.Fatal(err)
	}
	requireClosed(t, firstAttempt.Subscription.Messages)
	factory.controller(0).emit(event.Event{Kind: event.Notice, Text: "old-restored-after-abort"})
	if _, err := initial.Runtime.Snapshot(context.Background()); err != nil {
		t.Fatal(err)
	}
	if message := receiveMessage(t, initial.Subscription.Messages); message.Event == nil || message.Event.Event.Text != "old-restored-after-abort" {
		t.Fatalf("old subscription not restored after Abort: %+v", message)
	}

	retry, err := manager.Subscribe(context.Background(), testTarget(), attachment, initial.Subscription.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := retry.Commit(); err != nil {
		t.Fatal(err)
	}
	requireClosed(t, initial.Subscription.Messages)
	if _, err := manager.Subscribe(context.Background(), testTarget(), attachment, initial.Subscription.ID); !errors.Is(err, ErrSubscriptionNotFound) {
		t.Fatalf("committed old ID was reusable: %v", err)
	}
	factory.controller(0).emit(event.Event{Kind: event.Notice, Text: "committed-new-only"})
	if _, err := retry.Runtime.Snapshot(context.Background()); err != nil {
		t.Fatal(err)
	}
	if message := receiveMessage(t, retry.Subscription.Messages); message.Event == nil || message.Event.Event.Text != "committed-new-only" {
		t.Fatalf("committed subscription event = %+v", message)
	}
}

func TestManagedSubscriptionTransactionFinalizesAcrossRuntimeReplacement(t *testing.T) {
	for _, test := range []struct {
		name   string
		commit bool
	}{
		{name: "commit consumes old and retains new terminal", commit: true},
		{name: "abort consumes new and restores old terminal", commit: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			manager, _ := newTestRuntimeManager(t, context.Background(), 16, 64)
			attachment := testAttachment(1)
			initial, err := manager.Subscribe(context.Background(), testTarget(), attachment, "")
			if err != nil {
				t.Fatal(err)
			}
			if err := initial.Commit(); err != nil {
				t.Fatal(err)
			}
			pending, err := manager.Subscribe(context.Background(), testTarget(), attachment, initial.Subscription.ID)
			if err != nil {
				t.Fatal(err)
			}
			replacement, err := manager.Replace(testTarget())
			if err != nil {
				t.Fatal(err)
			}
			oldTerminal := receiveMessage(t, initial.Subscription.Messages)
			newTerminal := receiveMessage(t, pending.Subscription.Messages)
			if oldTerminal.Resync == nil || newTerminal.Resync == nil ||
				oldTerminal.Resync.ReplacementRuntimeEpoch != replacement.Epoch() || newTerminal.Resync.ReplacementRuntimeEpoch != replacement.Epoch() {
				t.Fatalf("replacement terminals = old %+v new %+v", oldTerminal, newTerminal)
			}

			if test.commit {
				if err := pending.Commit(); err != nil {
					t.Fatal(err)
				}
				requireClosed(t, initial.Subscription.Messages)
				select {
				case message := <-pending.Subscription.Messages:
					t.Fatalf("committed new terminal was prematurely closed: %+v", message)
				case <-time.After(25 * time.Millisecond):
				}
				migrated, err := manager.Subscribe(context.Background(), testTarget(), attachment, pending.Subscription.ID)
				if err != nil {
					t.Fatal(err)
				}
				if err := migrated.Commit(); err != nil {
					t.Fatal(err)
				}
				requireClosed(t, pending.Subscription.Messages)
			} else {
				if err := pending.Abort(); err != nil {
					t.Fatal(err)
				}
				requireClosed(t, pending.Subscription.Messages)
				select {
				case message := <-initial.Subscription.Messages:
					t.Fatalf("aborted old terminal was prematurely closed: %+v", message)
				case <-time.After(25 * time.Millisecond):
				}
				migrated, err := manager.Subscribe(context.Background(), testTarget(), attachment, initial.Subscription.ID)
				if err != nil {
					t.Fatal(err)
				}
				if err := migrated.Commit(); err != nil {
					t.Fatal(err)
				}
				requireClosed(t, initial.Subscription.Messages)
			}
		})
	}
}

func TestRuntimeReplacementPreservesSingleQueueOverflowTerminal(t *testing.T) {
	manager, factory := newTestRuntimeManager(t, context.Background(), 1, 32)
	attachment := testAttachment(1)
	runtime, err := manager.GetOrCreate(testTarget())
	if err != nil {
		t.Fatal(err)
	}
	subscription, err := runtime.Subscribe(context.Background(), attachment, "")
	if err != nil {
		t.Fatal(err)
	}
	factory.controller(0).emit(event.Event{Kind: event.Text, Text: "first"})
	factory.controller(0).emit(event.Event{Kind: event.Text, Text: "overflow"})
	if _, err := runtime.Snapshot(context.Background()); err != nil {
		t.Fatal(err)
	}
	terminal := receiveMessage(t, subscription.Messages)
	if terminal.Resync == nil || terminal.Resync.Reason != protocol.ResyncQueueOverflow {
		t.Fatalf("queue overflow terminal = %+v", terminal)
	}
	replacement, err := manager.Replace(testTarget())
	if err != nil {
		t.Fatal(err)
	}
	select {
	case message := <-subscription.Messages:
		t.Fatalf("replacement emitted a second resync after queue overflow: %+v", message)
	case <-time.After(25 * time.Millisecond):
	}
	migrated, err := manager.Subscribe(context.Background(), testTarget(), attachment, subscription.ID)
	if err != nil {
		t.Fatal(err)
	}
	if migrated.Runtime != replacement {
		t.Fatalf("overflow migration runtime = %p, want %p", migrated.Runtime, replacement)
	}
	if err := migrated.Commit(); err != nil {
		t.Fatal(err)
	}
	requireClosed(t, subscription.Messages)
}

func TestRuntimeEpochsAreNeverReusedAcrossReplacementOrTarget(t *testing.T) {
	factory := &fakeControllerFactory{}
	var epochCalls atomic.Int32
	manager, err := NewRuntimeManager(context.Background(), "host-test", factory, RuntimeManagerOptions{
		NewRuntimeEpoch: func() (protocol.RuntimeEpoch, error) {
			switch epochCalls.Add(1) {
			case 1, 2, 4:
				return "runtime-issued-once", nil
			case 3:
				return "runtime-replacement", nil
			default:
				return "runtime-other-target", nil
			}
		},
		NewTurnID:         func() (protocol.TurnID, error) { return "turn-fixed", nil },
		NewSubscriptionID: func() (protocol.SubscriptionID, error) { return "subscription-fixed", nil },
		SubscriptionQueue: 8,
		EventLogLimit:     32,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)

	first, err := manager.GetOrCreate(testTarget())
	if err != nil {
		t.Fatal(err)
	}
	replacement, err := manager.Replace(testTarget())
	if err != nil {
		t.Fatal(err)
	}
	otherTarget := protocol.RuntimeTarget{WorkspaceID: "workspace-test", SessionID: "session-other"}
	other, err := manager.GetOrCreate(otherTarget)
	if err != nil {
		t.Fatal(err)
	}
	if first.Epoch() != "runtime-issued-once" || replacement.Epoch() != "runtime-replacement" || other.Epoch() != "runtime-other-target" {
		t.Fatalf("runtime epochs = first %q replacement %q other %q", first.Epoch(), replacement.Epoch(), other.Epoch())
	}
	if got := epochCalls.Load(); got != 5 {
		t.Fatalf("runtime epoch generator calls = %d, want two collision retries", got)
	}
}

func TestSubscribeBarrierSequenceAndAtomicReplacement(t *testing.T) {
	manager, factory := newTestRuntimeManager(t, context.Background(), 128, 256)
	runtime, err := manager.GetOrCreate(testTarget())
	if err != nil {
		t.Fatal(err)
	}
	controller := factory.controller(0)

	half := make(chan struct{})
	resume := make(chan struct{})
	producerDone := make(chan struct{})
	richEvent := event.Event{
		Kind: event.Notice, Text: "rich notice", Detail: "complete diagnostic detail",
		Code: event.NoticeCodeLoopGuard, Level: event.LevelWarn,
	}
	go func() {
		for i := 1; i <= 50; i++ {
			value := event.Event{Kind: event.Notice, Text: fmt.Sprintf("before-%d", i)}
			if i == 25 {
				value = richEvent
			}
			controller.emit(value)
		}
		close(half)
		<-resume
		for i := 51; i <= 100; i++ {
			controller.emit(event.Event{Kind: event.Notice, Text: fmt.Sprintf("after-%d", i)})
		}
		close(producerDone)
	}()
	<-half

	attachment := testAttachment(1)
	subscription, err := runtime.Subscribe(context.Background(), attachment, "")
	if err != nil {
		t.Fatal(err)
	}
	if subscription.Snapshot.BoundarySeq != 50 || len(subscription.Snapshot.Events) != 50 {
		t.Fatalf("barrier snapshot = boundary %d events %d", subscription.Snapshot.BoundarySeq, len(subscription.Snapshot.Events))
	}
	if got, want := subscription.Snapshot.Events[24].Event, eventwire.ToWire(richEvent); !reflect.DeepEqual(got, want) {
		t.Fatalf("barrier lost rich event fields\n got: %#v\nwant: %#v", got, want)
	}
	close(resume)
	<-producerDone
	finalSnapshot, err := runtime.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if finalSnapshot.BoundarySeq != 100 {
		t.Fatalf("final boundary = %d, want 100", finalSnapshot.BoundarySeq)
	}
	for seq := uint64(51); seq <= 100; seq++ {
		message := receiveMessage(t, subscription.Messages)
		if message.Event == nil || message.Event.Seq != seq {
			t.Fatalf("post-barrier event = %+v, want seq %d", message, seq)
		}
	}

	replacement, err := runtime.Subscribe(context.Background(), attachment, subscription.ID)
	if err != nil {
		t.Fatal(err)
	}
	if replacement.Snapshot.BoundarySeq != 100 {
		t.Fatalf("replacement boundary = %d", replacement.Snapshot.BoundarySeq)
	}
	requireClosed(t, subscription.Messages)
	if _, err := runtime.Subscribe(context.Background(), testAttachment(2), replacement.ID); !errors.Is(err, ErrSubscriptionNotFound) {
		t.Fatalf("cross-attachment replacement error = %v", err)
	}
	controller.emit(event.Event{Kind: event.Notice, Text: "replacement-only"})
	snapshot, err := runtime.Snapshot(context.Background())
	if err != nil || snapshot.BoundarySeq != 101 {
		t.Fatalf("post-replacement snapshot = %+v, %v", snapshot, err)
	}
	message := receiveMessage(t, replacement.Messages)
	if message.Event == nil || message.Event.Seq != 101 {
		t.Fatalf("replacement event = %+v", message)
	}
}

func TestBoundedSubscriptionOverflowRequiresResubscribe(t *testing.T) {
	manager, factory := newTestRuntimeManager(t, context.Background(), 2, 64)
	runtime, err := manager.GetOrCreate(testTarget())
	if err != nil {
		t.Fatal(err)
	}
	controller := factory.controller(0)
	attachment := testAttachment(1)
	subscription, err := runtime.Subscribe(context.Background(), attachment, "")
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 3; i++ {
		controller.emit(event.Event{Kind: event.Text, Text: fmt.Sprintf("chunk-%d", i)})
	}
	if snapshot, err := runtime.Snapshot(context.Background()); err != nil || snapshot.BoundarySeq != 3 {
		t.Fatalf("overflow barrier = %+v, %v", snapshot, err)
	}
	message := receiveMessage(t, subscription.Messages)
	if message.Resync == nil || message.Event != nil || message.Resync.Reason != protocol.ResyncQueueOverflow || message.Resync.LastSeq != 2 {
		t.Fatalf("overflow message = %+v", message)
	}
	controller.emit(event.Event{Kind: event.Text, Text: "suppressed-after-overflow"})
	if _, err := runtime.Snapshot(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case message := <-subscription.Messages:
		t.Fatalf("overflowed subscription received another message: %+v", message)
	case <-time.After(25 * time.Millisecond):
	}

	replacement, err := runtime.Subscribe(context.Background(), attachment, subscription.ID)
	if err != nil {
		t.Fatal(err)
	}
	requireClosed(t, subscription.Messages)
	controller.emit(event.Event{Kind: event.Text, Text: "after-resubscribe"})
	if _, err := runtime.Snapshot(context.Background()); err != nil {
		t.Fatal(err)
	}
	if message := receiveMessage(t, replacement.Messages); message.Event == nil || message.Event.Seq != 5 {
		t.Fatalf("replacement did not resume at seq 5: %+v", message)
	}
}

func TestRuntimeReplacementChangesOnlyTargetEpochAndDropsLateOldEvents(t *testing.T) {
	manager, factory := newTestRuntimeManager(t, context.Background(), 16, 64)
	first, err := manager.GetOrCreate(testTarget())
	if err != nil {
		t.Fatal(err)
	}
	firstController := factory.controller(0)
	attachment := testAttachment(1)
	oldSubscription, err := first.Subscribe(context.Background(), attachment, "")
	if err != nil {
		t.Fatal(err)
	}
	firstController.emit(event.Event{Kind: event.Notice, Text: "old-current-event"})
	if snapshot, snapshotErr := first.Snapshot(context.Background()); snapshotErr != nil || snapshot.BoundarySeq != 1 {
		t.Fatalf("old snapshot = %+v, %v", snapshot, snapshotErr)
	}
	if message := receiveMessage(t, oldSubscription.Messages); message.Event == nil || message.Event.Seq != 1 {
		t.Fatalf("old current event = %+v", message)
	}

	replacement, err := manager.Replace(testTarget())
	if err != nil {
		t.Fatal(err)
	}
	if replacement.Epoch() == first.Epoch() {
		t.Fatalf("replacement retained runtime epoch %q", replacement.Epoch())
	}
	terminal := receiveMessage(t, oldSubscription.Messages)
	if terminal.Event != nil || terminal.Resync == nil || terminal.Resync.Reason != protocol.ResyncRuntimeReplaced ||
		terminal.Resync.Target != testTarget() || terminal.Resync.RuntimeEpoch != first.Epoch() ||
		terminal.Resync.ReplacementTarget != nil || terminal.Resync.ReplacementRuntimeEpoch != replacement.Epoch() || terminal.Resync.LastSeq != 1 {
		t.Fatalf("runtime replacement terminal = %+v", terminal)
	}
	select {
	case message, ok := <-oldSubscription.Messages:
		t.Fatalf("terminal subscription produced a second message: %+v, open=%v", message, ok)
	case <-time.After(25 * time.Millisecond):
	}
	closeCalls, _ := firstController.counts()
	if closeCalls != 1 {
		t.Fatalf("replaced Controller Close calls = %d, want 1", closeCalls)
	}

	firstController.emit(event.Event{Kind: event.Notice, Text: "late-old-event"})
	snapshot, err := replacement.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.BoundarySeq != 0 || len(snapshot.Events) != 0 {
		t.Fatalf("late old event entered replacement: %+v", snapshot)
	}

	install, err := manager.Subscribe(context.Background(), testTarget(), attachment, oldSubscription.ID)
	if err != nil {
		t.Fatal(err)
	}
	if install.Runtime != replacement || install.Subscription.Snapshot.RuntimeEpoch != replacement.Epoch() || install.Subscription.Snapshot.BoundarySeq != 0 {
		t.Fatalf("replacement install = runtime %p snapshot %+v", install.Runtime, install.Subscription.Snapshot)
	}
	install.Commit()
	requireClosed(t, oldSubscription.Messages)
	factory.controller(1).emit(event.Event{Kind: event.Notice, Text: "new-runtime-only"})
	if next := receiveMessage(t, install.Subscription.Messages); next.Event == nil || next.Event.Seq != 1 || next.Event.Event.Text != "new-runtime-only" {
		t.Fatalf("new runtime event = %+v", next)
	}
}

func TestTargetReplacementRetainsAndMigratesExactSubscriptionIdentity(t *testing.T) {
	manager, factory := newTestRuntimeManager(t, context.Background(), 16, 64)
	oldTarget := testTarget()
	newTarget := protocol.RuntimeTarget{WorkspaceID: oldTarget.WorkspaceID, SessionID: "session-moved"}
	attachment := testAttachment(1)
	oldRuntime, err := manager.GetOrCreate(oldTarget)
	if err != nil {
		t.Fatal(err)
	}
	oldSubscription, err := oldRuntime.Subscribe(context.Background(), attachment, "")
	if err != nil {
		t.Fatal(err)
	}

	replacement, err := manager.ReplaceTarget(oldTarget, newTarget)
	if err != nil {
		t.Fatal(err)
	}
	terminal := receiveMessage(t, oldSubscription.Messages)
	if terminal.Resync == nil || terminal.Resync.Reason != protocol.ResyncTargetReplaced || terminal.Resync.ReplacementTarget == nil ||
		*terminal.Resync.ReplacementTarget != newTarget || terminal.Resync.ReplacementRuntimeEpoch != replacement.Epoch() {
		t.Fatalf("target replacement terminal = %+v", terminal)
	}
	if _, exists := manager.Runtime(oldTarget); exists {
		t.Fatal("old target remained current after target replacement")
	}
	if current, exists := manager.Runtime(newTarget); !exists || current != replacement {
		t.Fatalf("replacement target current = %p, exists=%v", current, exists)
	}
	if _, err := manager.Subscribe(context.Background(), oldTarget, attachment, oldSubscription.ID); !errors.Is(err, ErrSubscriptionNotFound) {
		t.Fatalf("old-target migration error = %v", err)
	}
	install, err := manager.Subscribe(context.Background(), newTarget, attachment, oldSubscription.ID)
	if err != nil {
		t.Fatal(err)
	}
	install.Commit()
	requireClosed(t, oldSubscription.Messages)
	factory.controller(1).emit(event.Event{Kind: event.Notice, Text: "moved-target-event"})
	if message := receiveMessage(t, install.Subscription.Messages); message.Event == nil || message.Event.Target != newTarget {
		t.Fatalf("moved target event = %+v", message)
	}
}

func TestReplacementSealOrdersAdmissionBeforeRequestIDAndControllerSideEffects(t *testing.T) {
	manager, factory := newTestRuntimeManager(t, context.Background(), 16, 64)
	runtime, err := manager.GetOrCreate(testTarget())
	if err != nil {
		t.Fatal(err)
	}
	oldSubscription, err := runtime.Subscribe(context.Background(), testAttachment(1), "")
	if err != nil {
		t.Fatal(err)
	}
	registry, err := idempotency.New("host-test", idempotency.Options{})
	if err != nil {
		t.Fatal(err)
	}
	unblock := blockRuntimeActor(t, runtime)
	before := protocol.SessionSubmitParams{
		SessionMutation: protocol.SessionMutation{
			RequestID: "request-before-replacement", ExpectedHostEpoch: "host-test",
			Target: testTarget(), ExpectedRuntimeEpoch: runtime.Epoch(),
		},
		Input: "accepted before replacement", DisplayText: "before replacement",
	}
	type submitOutcome struct {
		result protocol.SessionSubmitResult
		err    error
	}
	beforeDone := make(chan submitOutcome, 1)
	go func() {
		result, callErr := runtime.SubmitMutation(context.Background(), registry, before, nil, nil)
		beforeDone <- submitOutcome{result: result, err: callErr}
	}()
	waitRuntimeMailboxQueued(t, runtime)

	type replaceOutcome struct {
		runtime *SessionRuntime
		err     error
	}
	replaceDone := make(chan replaceOutcome, 1)
	go func() {
		replaced, replaceErr := manager.Replace(testTarget())
		replaceDone <- replaceOutcome{runtime: replaced, err: replaceErr}
	}()
	waitRuntimeMailboxSealed(t, runtime)

	after := before
	after.RequestID = "request-after-replacement-seal"
	after.Input = "must never reach old Controller"
	after.DisplayText = "must never reach old Controller"
	if result, callErr := runtime.SubmitMutation(context.Background(), registry, after, nil, nil); !errors.Is(callErr, ErrRuntimeClosed) || result != (protocol.SessionSubmitResult{}) {
		t.Fatalf("post-seal old Submit = %+v, %v", result, callErr)
	}
	if stats := registry.Stats(); stats.Entries != 0 {
		t.Fatalf("post-seal call created requestId before actor release: %+v", stats)
	}
	unblock()

	select {
	case outcome := <-beforeDone:
		if outcome.err != nil || outcome.result.TurnID == "" {
			t.Fatalf("pre-seal Submit = %+v, %v", outcome.result, outcome.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("pre-seal Submit did not complete")
	}
	var replacement *SessionRuntime
	select {
	case outcome := <-replaceDone:
		if outcome.err != nil {
			t.Fatal(outcome.err)
		}
		replacement = outcome.runtime
	case <-time.After(2 * time.Second):
		t.Fatal("replacement deadlocked behind actor barrier")
	}
	if factory.controller(0).strictSubmitCount() != 1 || registry.Stats().Entries != 1 {
		t.Fatalf("old admission side effects = Controller %d registry %+v", factory.controller(0).strictSubmitCount(), registry.Stats())
	}
	terminal := receiveMessage(t, oldSubscription.Messages)
	if terminal.Resync == nil || terminal.Resync.Reason != protocol.ResyncRuntimeReplaced || terminal.Resync.LastSeq != 1 {
		t.Fatalf("pre-seal synchronous event terminal = %+v", terminal)
	}
	after.ExpectedRuntimeEpoch = replacement.Epoch()
	result, err := replacement.SubmitMutation(context.Background(), registry, after, nil, nil)
	if err != nil || result.TurnID == "" || factory.controller(1).strictSubmitCount() != 1 {
		t.Fatalf("same requestId on replacement = %+v, %v; Controller calls=%d", result, err, factory.controller(1).strictSubmitCount())
	}
	factory.controller(1).releaseTurn()
}

func TestReplacementCommitDropsOldEventAlreadyQueuedBehindCommit(t *testing.T) {
	manager, factory := newTestRuntimeManager(t, context.Background(), 8, 32)
	runtime, err := manager.GetOrCreate(testTarget())
	if err != nil {
		t.Fatal(err)
	}
	subscription, err := runtime.Subscribe(context.Background(), testAttachment(1), "")
	if err != nil {
		t.Fatal(err)
	}
	firstUnblock := blockRuntimeActor(t, runtime)
	replaceDone := make(chan *SessionRuntime, 1)
	replaceErr := make(chan error, 1)
	go func() {
		replacement, callErr := manager.Replace(testTarget())
		if callErr != nil {
			replaceErr <- callErr
			return
		}
		replaceDone <- replacement
	}()
	waitRuntimeMailboxSealed(t, runtime)

	betweenEntered := make(chan struct{})
	betweenRelease := make(chan struct{})
	if !runtime.mailbox.enqueue(func(*runtimeActorState) {
		close(betweenEntered)
		<-betweenRelease
	}) {
		t.Fatal("failed to enqueue between-seal-and-commit blocker")
	}
	firstUnblock()
	select {
	case <-betweenEntered:
	case err := <-replaceErr:
		t.Fatal(err)
	case <-time.After(2 * time.Second):
		t.Fatal("replacement intent did not enqueue commit behind test blocker")
	}
	// The sink observes current=true before commit and successfully queues this
	// event behind the already-enqueued commit. The actor must recheck current
	// at execution/teardown and drop it without consuming seq or broadcasting.
	factory.controller(0).emit(event.Event{Kind: event.Notice, Text: "queued-behind-replacement-commit"})
	close(betweenRelease)

	var replacement *SessionRuntime
	select {
	case replacement = <-replaceDone:
	case err := <-replaceErr:
		t.Fatal(err)
	case <-time.After(2 * time.Second):
		t.Fatal("replacement did not finish")
	}
	terminal := receiveMessage(t, subscription.Messages)
	if terminal.Event != nil || terminal.Resync == nil || terminal.Resync.LastSeq != 0 || terminal.Resync.Reason != protocol.ResyncRuntimeReplaced {
		t.Fatalf("terminal after queued late event = %+v", terminal)
	}
	snapshot, err := replacement.Snapshot(context.Background())
	if err != nil || snapshot.BoundarySeq != 0 || len(snapshot.Events) != 0 {
		t.Fatalf("queued old event affected replacement = %+v, %v", snapshot, err)
	}
	select {
	case message := <-subscription.Messages:
		t.Fatalf("queued old event reached terminal subscription: %+v", message)
	case <-time.After(25 * time.Millisecond):
	}
}

func waitRuntimeMailboxSealed(t *testing.T, runtime *SessionRuntime) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		runtime.mailbox.mu.Lock()
		sealed := runtime.mailbox.sealed
		closed := runtime.mailbox.closed
		runtime.mailbox.mu.Unlock()
		if sealed {
			return
		}
		if closed {
			t.Fatal("runtime mailbox closed before replacement seal")
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("replacement did not seal actor admission")
}

package host

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/command"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/eventwire"
	"reasonix/internal/plugin"
	"reasonix/internal/provider"
	"reasonix/internal/remote/idempotency"
	"reasonix/internal/remote/protocol"
	"reasonix/internal/skill"
	"reasonix/internal/tool"
)

type composerController struct {
	*fakeSessionController

	composerMu  sync.Mutex
	calls       []string
	displays    []string
	originals   []string
	invocations [][]control.InvocationRequest
}

func (c *composerController) WorkspaceRoot() string {
	return c.fakeSessionController.ctx.Value(composerRootKey{}).(string)
}
func (c *composerController) Commands() []command.Command {
	return []command.Command{{Name: "deploy"}}
}
func (c *composerController) Host() *plugin.Host { return nil }
func (c *composerController) SlashSkills() []skill.Skill {
	return []skill.Skill{
		{Name: "review", RunAs: skill.RunInline},
		{Name: "delegate", RunAs: skill.RunSubagent},
	}
}

func (c *composerController) SubmitDisplay(display, input string) {
	c.record("display", display, "", nil)
	if strings.HasPrefix(strings.TrimSpace(input), "/") && !strings.HasPrefix(strings.TrimSpace(input), "/deploy") &&
		!strings.HasPrefix(strings.TrimSpace(input), "/goal ") {
		c.sink.Emit(event.Event{Kind: event.Notice, Text: "read-only or unknown"})
		return
	}
	c.Submit(input)
}

func (c *composerController) SubmitEditedDisplay(display, input, original string) {
	c.record("edited", display, original, nil)
	c.Submit(input)
}

func (c *composerController) SubmitDeliveryRecovery(display, input string) {
	c.record("recovery", display, "", nil)
	c.Submit(input)
}

func (c *composerController) SubmitInvocationDisplay(display, input string, requests []control.InvocationRequest) {
	c.record("invocations", display, "", requests)
	c.Submit(input)
}

func (c *composerController) record(kind, display, original string, requests []control.InvocationRequest) {
	c.composerMu.Lock()
	defer c.composerMu.Unlock()
	c.calls = append(c.calls, kind)
	c.displays = append(c.displays, display)
	c.originals = append(c.originals, original)
	if requests != nil {
		copyRequests := append([]control.InvocationRequest(nil), requests...)
		c.invocations = append(c.invocations, copyRequests)
	}
}

type composerRootKey struct{}

type durableFailureProvider struct{ calls atomic.Int32 }

func (p *durableFailureProvider) Name() string { return "durable-failure" }
func (p *durableFailureProvider) Stream(context.Context, provider.Request) (<-chan provider.Chunk, error) {
	p.calls.Add(1)
	stream := make(chan provider.Chunk, 2)
	stream <- provider.Chunk{Type: provider.ChunkText, Text: "persisted response"}
	stream <- provider.Chunk{Type: provider.ChunkDone}
	close(stream)
	return stream, nil
}

type durableFailureFactory struct {
	badPath    string
	provider   *durableFailureProvider
	controller *control.Controller
}

type committedFailureController struct {
	*composerController
	path          string
	prepareCalls  atomic.Int32
	submitCalls   atomic.Int32
	providerCalls atomic.Int32
	prepared      []control.DurableTurnInput
	order         []string
}

func (c *committedFailureController) EnableDurableTurnAdmission() error { return nil }

func (c *committedFailureController) PrepareDurableTurn(input control.DurableTurnInput) (func() control.DurableTurnAdmissionResult, error) {
	c.prepareCalls.Add(1)
	c.composerMu.Lock()
	c.prepared = append(c.prepared, input)
	c.order = append(c.order, "prepare")
	c.composerMu.Unlock()
	session := agent.NewSession("system")
	session.Add(provider.Message{Role: provider.RoleUser, Content: input.Input})
	if err := session.SaveSnapshot(c.path); err != nil {
		return nil, err
	}
	return func() control.DurableTurnAdmissionResult {
		c.composerMu.Lock()
		c.order = append(c.order, "complete")
		c.composerMu.Unlock()
		return control.DurableTurnAdmissionResult{
			Claimed: true, SemanticCommit: true, Err: errors.New("injected listing sidecar failure"),
		}
	}, nil
}

func (c *committedFailureController) SubmitDisplay(display, input string) {
	c.record("display", display, "", nil)
	c.executePreparedTurn()
}

func (c *committedFailureController) SubmitEditedDisplay(display, input, original string) {
	c.record("edited", display, original, nil)
	c.executePreparedTurn()
}

func (c *committedFailureController) executePreparedTurn() {
	c.submitCalls.Add(1)
	c.composerMu.Lock()
	c.order = append(c.order, "submit")
	c.composerMu.Unlock()
	c.providerCalls.Add(1)
	c.sink.Emit(event.Event{Kind: event.TurnStarted})
	c.sink.Emit(event.Event{Kind: event.TurnDone})
}

type committedFailureFactory struct {
	root       string
	path       string
	controller *committedFailureController
}

func (f *committedFailureFactory) CreateController(ctx context.Context, _ protocol.RuntimeTarget, sink event.Sink) (control.SessionAPI, error) {
	ctx = context.WithValue(ctx, composerRootKey{}, f.root)
	f.controller = &committedFailureController{
		composerController: &composerController{fakeSessionController: newFakeSessionController(ctx, sink)},
		path:               f.path,
	}
	return f.controller, nil
}

type unclaimedThenSuccessController struct {
	*composerController
	attempt atomic.Int32
}

func (c *unclaimedThenSuccessController) EnableDurableTurnAdmission() error { return nil }

func (c *unclaimedThenSuccessController) PrepareDurableTurn(control.DurableTurnInput) (func() control.DurableTurnAdmissionResult, error) {
	attempt := c.attempt.Add(1)
	if attempt == 1 {
		// This event models unrelated background work queued while the actor is
		// synchronously preparing a Turn that ultimately fails before commit.
		// It must survive without positional Turn-segment suppression.
		c.sink.Emit(event.Event{Kind: event.Notice, Text: "background event during failed prepare"})
		return nil, errors.New("injected durable prepare failure")
	}
	return func() control.DurableTurnAdmissionResult {
		return control.DurableTurnAdmissionResult{Claimed: true, SemanticCommit: true}
	}, nil
}

func (c *unclaimedThenSuccessController) SubmitDisplay(display, input string) {
	c.record("display", display, "", nil)
	if c.attempt.Load() == 1 {
		return
	}
	c.sink.Emit(event.Event{Kind: event.TurnStarted})
	c.sink.Emit(event.Event{Kind: event.TurnDone})
}

type unclaimedThenSuccessFactory struct {
	root       string
	controller *unclaimedThenSuccessController
}

type postCommitInvariantController struct {
	*composerController
	path        string
	claimed     bool
	panicSubmit bool
	prepares    atomic.Int32
	submits     atomic.Int32
	submitPanic atomic.Bool
}

func (c *postCommitInvariantController) EnableDurableTurnAdmission() error { return nil }

func (c *postCommitInvariantController) PrepareDurableTurn(input control.DurableTurnInput) (func() control.DurableTurnAdmissionResult, error) {
	c.prepares.Add(1)
	session, err := agent.LoadSession(c.path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		session = agent.NewSession("system")
	}
	startMessages := session.Len()
	session.Add(provider.Message{Role: provider.RoleUser, Content: input.Input})
	if err := session.SaveSnapshot(c.path); err != nil {
		return nil, err
	}
	if err := agent.MarkSessionInFlightTurn(c.path, startMessages, true); err != nil {
		return nil, err
	}
	return func() control.DurableTurnAdmissionResult {
		claimed := c.claimed && !c.submitPanic.Load()
		if !claimed {
			if err := agent.ClearSessionInFlightTurn(c.path); err != nil {
				return control.DurableTurnAdmissionResult{SemanticCommit: true, Err: err}
			}
			return control.DurableTurnAdmissionResult{
				SemanticCommit: true, Err: errors.New("prepared durable Turn was not claimed"),
			}
		}
		return control.DurableTurnAdmissionResult{Claimed: true, SemanticCommit: true}
	}, nil
}

func (c *postCommitInvariantController) SubmitDisplay(display, input string) {
	c.record("display", display, "", nil)
	c.submits.Add(1)
	if c.panicSubmit {
		c.submitPanic.Store(true)
		panic("injected post-commit Submit panic")
	}
}

type postCommitInvariantFactory struct {
	root        string
	path        string
	claimed     bool
	panicSubmit bool
	controller  *postCommitInvariantController
}

func (f *postCommitInvariantFactory) CreateController(ctx context.Context, _ protocol.RuntimeTarget, sink event.Sink) (control.SessionAPI, error) {
	ctx = context.WithValue(ctx, composerRootKey{}, f.root)
	f.controller = &postCommitInvariantController{
		composerController: &composerController{fakeSessionController: newFakeSessionController(ctx, sink)},
		path:               f.path, claimed: f.claimed, panicSubmit: f.panicSubmit,
	}
	return f.controller, nil
}

func (f *unclaimedThenSuccessFactory) CreateController(ctx context.Context, _ protocol.RuntimeTarget, sink event.Sink) (control.SessionAPI, error) {
	ctx = context.WithValue(ctx, composerRootKey{}, f.root)
	f.controller = &unclaimedThenSuccessController{
		composerController: &composerController{fakeSessionController: newFakeSessionController(ctx, sink)},
	}
	return f.controller, nil
}

func (f *durableFailureFactory) CreateController(_ context.Context, _ protocol.RuntimeTarget, sink event.Sink) (control.SessionAPI, error) {
	executor := agent.New(f.provider, tool.NewRegistry(), agent.NewSession("system"), agent.Options{}, sink)
	controller := control.New(control.Options{
		Runner: executor, Executor: executor, Sink: sink,
		SessionDir: filepath.Dir(f.badPath), SessionPath: f.badPath,
	})
	if err := controller.EnableDurableTurnAdmission(); err != nil {
		return nil, err
	}
	f.controller = controller
	return controller, nil
}

type composerFactory struct {
	root       string
	mu         sync.Mutex
	controller *composerController
}

func (f *composerFactory) CreateController(ctx context.Context, _ protocol.RuntimeTarget, sink event.Sink) (control.SessionAPI, error) {
	ctx = context.WithValue(ctx, composerRootKey{}, f.root)
	controller := &composerController{fakeSessionController: newFakeSessionController(ctx, sink)}
	f.mu.Lock()
	f.controller = controller
	f.mu.Unlock()
	return controller, nil
}

func newComposerRuntime(t *testing.T) (*RuntimeManager, *SessionRuntime, *composerController, *idempotency.Registry) {
	t.Helper()
	factory := &composerFactory{root: t.TempDir()}
	manager, err := NewRuntimeManager(context.Background(), "host-test", factory, RuntimeManagerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	runtime, err := manager.GetOrCreate(testTarget())
	if err != nil {
		t.Fatal(err)
	}
	registry, err := idempotency.New("host-test", idempotency.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return manager, runtime, factory.controller, registry
}

func composerParams(runtime *SessionRuntime, requestID, input string) protocol.SessionSubmitParams {
	return protocol.SessionSubmitParams{
		SessionMutation: protocol.SessionMutation{
			RequestID: protocol.RequestID(requestID), ExpectedHostEpoch: "host-test", Target: testTarget(), ExpectedRuntimeEpoch: runtime.Epoch(),
		},
		Input: input, DisplayText: input,
	}
}

func TestComposerSubmitMutationUsesExactTurnPrimitiveAndReplaysAdmission(t *testing.T) {
	_, runtime, controller, registry := newComposerRuntime(t)
	params := composerParams(runtime, "composer-edited", "expanded")
	params.DisplayText = "visible"
	params.EditedOriginal = "before"

	result, err := runtime.ComposerSubmitMutation(context.Background(), registry, params, nil)
	if err != nil || result.Kind != protocol.SubmitTurn || result.TurnID == "" {
		t.Fatalf("ComposerSubmitMutation = %+v, %v", result, err)
	}
	replay, err := runtime.ComposerSubmitMutation(context.Background(), registry, params, nil)
	if err != nil || replay != result {
		t.Fatalf("replay = %+v, %v; want %+v", replay, err, result)
	}
	controller.composerMu.Lock()
	calls := append([]string(nil), controller.calls...)
	displays := append([]string(nil), controller.displays...)
	originals := append([]string(nil), controller.originals...)
	controller.composerMu.Unlock()
	if !reflect.DeepEqual(calls, []string{"edited"}) || !reflect.DeepEqual(displays, []string{"visible"}) ||
		!reflect.DeepEqual(originals, []string{"before"}) {
		t.Fatalf("controller calls=%v displays=%v originals=%v", calls, displays, originals)
	}
	controller.releaseTurn()
}

func TestComposerDurableAdmissionFailureMapsAndSameRequestRetriesAfterRepair(t *testing.T) {
	providerValue := &durableFailureProvider{}
	factory := &durableFailureFactory{badPath: t.TempDir(), provider: providerValue}
	manager, err := NewRuntimeManager(context.Background(), "host-test", factory, RuntimeManagerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	runtime, err := manager.GetOrCreate(testTarget())
	if err != nil {
		t.Fatal(err)
	}
	attachment := testAttachment(77)
	initial, err := manager.SubscribeSnapshot(context.Background(), testTarget(), attachment, "", "snapshot-before-durable-failure")
	if err != nil {
		t.Fatal(err)
	}
	if err := initial.Commit(); err != nil {
		t.Fatal(err)
	}
	baseline := initial.Subscription.Snapshot
	registry, err := idempotency.New("host-test", idempotency.Options{})
	if err != nil {
		t.Fatal(err)
	}
	params := composerParams(runtime, "durable-retry", "persist before provider")
	if _, err := runtime.ComposerSubmitMutation(context.Background(), registry, params, nil); err == nil {
		t.Fatal("durable persistence failure returned success")
	} else {
		var remote *protocol.RemoteError
		if !errors.As(err, &remote) || remote.Code != protocol.ErrSessionPersistFailed || remote.Data.Target == nil || *remote.Data.Target != testTarget() {
			t.Fatalf("durable persistence error = %#v", err)
		}
	}
	if factory.controller.Running() {
		t.Fatal("failed durable response returned before Controller cleanup completed")
	}
	if calls := providerValue.calls.Load(); calls != 0 {
		t.Fatalf("provider calls after failed admission = %d, want 0", calls)
	}
	if history := factory.controller.History(); len(history) != 1 || history[0].Role != provider.RoleSystem {
		t.Fatalf("failed admission history = %+v", history)
	}
	afterFailure, err := manager.SubscribeSnapshot(
		context.Background(), testTarget(), attachment, initial.Subscription.ID, "snapshot-after-durable-failure",
	)
	if err != nil {
		t.Fatal(err)
	}
	failedSnapshot := afterFailure.Subscription.Snapshot
	if failedSnapshot.BoundarySeq != baseline.BoundarySeq || failedSnapshot.Running || failedSnapshot.CurrentTurn != "" ||
		failedSnapshot.LastOutcome != baseline.LastOutcome || failedSnapshot.LastError != baseline.LastError ||
		!reflect.DeepEqual(failedSnapshot.Capture.History.Messages, baseline.Capture.History.Messages) ||
		!reflect.DeepEqual(failedSnapshot.Capture.Checkpoints, baseline.Capture.Checkpoints) {
		t.Fatalf("failed admission changed Host snapshot:\nbefore=%+v\nafter=%+v", baseline, failedSnapshot)
	}
	select {
	case message := <-initial.Subscription.Messages:
		t.Fatalf("failed admission published subscription message: %+v", message)
	default:
	}
	if err := afterFailure.Abort(); err != nil {
		t.Fatal(err)
	}

	factory.controller.SetSessionPath(filepath.Join(t.TempDir(), "repaired.jsonl"))
	result, err := runtime.ComposerSubmitMutation(context.Background(), registry, params, nil)
	if err != nil || result.Kind != protocol.SubmitTurn || result.TurnID == "" {
		t.Fatalf("retry after persistence repair = %+v, %v", result, err)
	}
	replay, err := runtime.ComposerSubmitMutation(context.Background(), registry, params, nil)
	if err != nil || replay != result {
		t.Fatalf("completed requestId replay = %+v, %v; want %+v", replay, err, result)
	}
	deadline := time.Now().Add(3 * time.Second)
	for providerValue.calls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if calls := providerValue.calls.Load(); calls != 1 {
		t.Fatalf("provider calls after repaired retry and replay = %d, want 1", calls)
	}
}

func TestComposerCommittedTranscriptSidecarFailureSucceedsAndReplaysWithoutDuplicateExecution(t *testing.T) {
	dir := t.TempDir()
	factory := &committedFailureFactory{root: dir, path: filepath.Join(dir, "partial-commit.jsonl")}
	manager, err := NewRuntimeManager(context.Background(), "host-test", factory, RuntimeManagerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	runtime, err := manager.GetOrCreate(testTarget())
	if err != nil {
		t.Fatal(err)
	}
	registry, err := idempotency.New("host-test", idempotency.Options{})
	if err != nil {
		t.Fatal(err)
	}
	params := composerParams(runtime, "committed-sidecar", "execute exactly once")
	params.DisplayText = "visible accepted prompt"
	params.EditedOriginal = "original accepted prompt"
	result, err := runtime.ComposerSubmitMutation(context.Background(), registry, params, nil)
	if err != nil || result.Kind != protocol.SubmitTurn || result.TurnID == "" {
		t.Fatalf("partial-commit admission = %+v, %v", result, err)
	}
	replay, err := runtime.ComposerSubmitMutation(context.Background(), registry, params, nil)
	if err != nil || replay != result {
		t.Fatalf("partial-commit replay = %+v, %v; want %+v", replay, err, result)
	}
	if calls := factory.controller.submitCalls.Load(); calls != 1 {
		t.Fatalf("Controller submits = %d, want 1", calls)
	}
	if calls := factory.controller.prepareCalls.Load(); calls != 1 {
		t.Fatalf("Controller durable prepares = %d, want 1", calls)
	}
	factory.controller.composerMu.Lock()
	prepared := append([]control.DurableTurnInput(nil), factory.controller.prepared...)
	order := append([]string(nil), factory.controller.order...)
	factory.controller.composerMu.Unlock()
	wantPrepared := control.DurableTurnInput{
		Input: params.Input, DisplayText: params.DisplayText, EditedOriginal: params.EditedOriginal,
	}
	if !reflect.DeepEqual(prepared, []control.DurableTurnInput{wantPrepared}) ||
		!reflect.DeepEqual(order, []string{"prepare", "submit", "complete"}) {
		t.Fatalf("durable prepare input/order = %+v / %v", prepared, order)
	}
	if calls := factory.controller.providerCalls.Load(); calls != 1 {
		t.Fatalf("provider executions = %d, want 1", calls)
	}
	loaded, err := agent.LoadSession(factory.path)
	if err != nil {
		t.Fatal(err)
	}
	messages := loaded.Snapshot()
	if len(messages) != 2 || messages[0].Role != provider.RoleSystem || messages[1].Role != provider.RoleUser || messages[1].Content != params.Input {
		t.Fatalf("partial-commit disk transcript = %+v", messages)
	}
	if stats := registry.Stats(); stats.Completed != 1 || stats.Pending != 0 {
		t.Fatalf("partial-commit registry stats = %+v", stats)
	}
}

func TestComposerPrepareFailureDoesNotSuppressBackgroundOrNextRetryEvents(t *testing.T) {
	factory := &unclaimedThenSuccessFactory{root: t.TempDir()}
	manager, err := NewRuntimeManager(context.Background(), "host-test", factory, RuntimeManagerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	runtime, err := manager.GetOrCreate(testTarget())
	if err != nil {
		t.Fatal(err)
	}
	registry, err := idempotency.New("host-test", idempotency.Options{})
	if err != nil {
		t.Fatal(err)
	}
	initial, err := manager.SubscribeSnapshot(
		context.Background(), testTarget(), testAttachment(88), "", "prepare-failure-background-event",
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := initial.Commit(); err != nil {
		t.Fatal(err)
	}
	params := composerParams(runtime, "unclaimed-retry", "retry after unclaimed")
	if _, err := runtime.ComposerSubmitMutation(context.Background(), registry, params, nil); err == nil {
		t.Fatal("failed durable prepare unexpectedly succeeded")
	}
	select {
	case message := <-initial.Subscription.Messages:
		if message.Event == nil || message.Event.Event.Kind != eventwire.ToWire(event.Event{Kind: event.Notice}).Kind ||
			message.Event.Event.Text != "background event during failed prepare" {
			t.Fatalf("event queued during failed prepare = %+v", message)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("failed durable prepare suppressed an unrelated background event")
	}
	result, err := runtime.ComposerSubmitMutation(context.Background(), registry, params, nil)
	if err != nil || result.Kind != protocol.SubmitTurn {
		t.Fatalf("retry after unclaimed admission = %+v, %v", result, err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		admission, snapshotErr := manager.SubscribeSnapshot(context.Background(), testTarget(), testAttachment(89), "", "prepare-retry-event-check")
		if snapshotErr != nil {
			t.Fatal(snapshotErr)
		}
		snapshot := admission.Subscription.Snapshot
		if err := admission.Abort(); err != nil {
			t.Fatal(err)
		}
		if snapshot.BoundarySeq >= 2 && snapshot.LastOutcome == protocol.OutcomeCompleted && !snapshot.Running {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("successful retry events remained suppressed after an unclaimed failure")
}

func TestComposerPostCommitDispatchInvariantFinalizesMarkerAndAllowsNextTurn(t *testing.T) {
	for _, tc := range []struct {
		name        string
		claimed     bool
		panicSubmit bool
	}{
		{name: "prepared but unclaimed"},
		{name: "Submit panic", claimed: true, panicSubmit: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "prepared.jsonl")
			factory := &postCommitInvariantFactory{
				root: root, path: path, claimed: tc.claimed, panicSubmit: tc.panicSubmit,
			}
			manager, err := NewRuntimeManager(context.Background(), "host-test", factory, RuntimeManagerOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer manager.Close()
			runtime, err := manager.GetOrCreate(testTarget())
			if err != nil {
				t.Fatal(err)
			}
			registry, err := idempotency.New("host-test", idempotency.Options{})
			if err != nil {
				t.Fatal(err)
			}

			params := composerParams(runtime, "post-commit-invariant", "durably accepted")
			result, err := runtime.ComposerSubmitMutation(context.Background(), registry, params, nil)
			if err != nil || result.Kind != protocol.SubmitTurn || result.TurnID == "" {
				t.Fatalf("post-commit invariant result = %+v, %v", result, err)
			}
			replay, err := runtime.ComposerSubmitMutation(context.Background(), registry, params, nil)
			if err != nil || replay != result {
				t.Fatalf("post-commit exact replay = %+v, %v; want %+v", replay, err, result)
			}

			meta, ok, err := agent.LoadBranchMeta(path)
			if err != nil || !ok || meta.InFlightTurn != nil {
				t.Fatalf("post-commit invariant left marker after completion cleanup: ok=%v err=%v meta=%+v", ok, err, meta)
			}
			next := composerParams(runtime, "post-commit-next", "next durable turn")
			nextResult, err := runtime.ComposerSubmitMutation(context.Background(), registry, next, nil)
			if err != nil || nextResult.Kind != protocol.SubmitTurn || nextResult.TurnID == "" || nextResult.TurnID == result.TurnID {
				t.Fatalf("next Turn after completed cleanup = %+v, %v", nextResult, err)
			}
			if prepares := factory.controller.prepares.Load(); prepares != 2 {
				t.Fatalf("durable prepares after next Turn = %d, want 2", prepares)
			}
			if submits := factory.controller.submits.Load(); submits != 2 {
				t.Fatalf("Controller submits after next Turn = %d, want 2", submits)
			}
			meta, ok, err = agent.LoadBranchMeta(path)
			if err != nil || !ok || meta.InFlightTurn != nil {
				t.Fatalf("next post-commit invariant left marker: ok=%v err=%v meta=%+v", ok, err, meta)
			}
		})
	}
}

func TestComposerSubmitMutationPreservesInvocationArrayOrderAsOffsets(t *testing.T) {
	_, runtime, controller, registry := newComposerRuntime(t)
	params := composerParams(runtime, "composer-invocations", "review this")
	params.Invocations = []protocol.Invocation{
		{Name: "delegate", Kind: protocol.InvocationSubagent},
		{Name: "review", Kind: protocol.InvocationSkill},
	}
	result, err := runtime.ComposerSubmitMutation(context.Background(), registry, params, nil)
	if err != nil || result.Kind != protocol.SubmitTurn {
		t.Fatalf("ComposerSubmitMutation = %+v, %v", result, err)
	}
	controller.composerMu.Lock()
	requests := append([]control.InvocationRequest(nil), controller.invocations[0]...)
	controller.composerMu.Unlock()
	want := []control.InvocationRequest{
		{Name: "delegate", Kind: "subagent", Offset: 0},
		{Name: "review", Kind: "skill", Offset: 1},
	}
	if !reflect.DeepEqual(requests, want) {
		t.Fatalf("requests = %+v, want %+v", requests, want)
	}
	controller.releaseTurn()
}

func TestComposerSubmitMutationReturnsOperationUnionForShell(t *testing.T) {
	_, runtime, controller, registry := newComposerRuntime(t)
	params := composerParams(runtime, "composer-shell", "! printf remote-v1")
	result, err := runtime.ComposerSubmitMutation(context.Background(), registry, params, nil)
	if err != nil || result.Kind != protocol.SubmitOperation || result.Operation != protocol.OperationShell || result.OperationID == "" {
		t.Fatalf("ComposerSubmitMutation = %+v, %v", result, err)
	}
	controller.mu.Lock()
	specs := append([]control.OperationSpec(nil), controller.operationSpecs...)
	controller.mu.Unlock()
	if len(specs) != 1 || specs[0].Kind != control.OperationShell || specs[0].Command != "printf remote-v1" {
		t.Fatalf("operation specs = %+v", specs)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		snapshot, snapErr := runtime.Snapshot(context.Background())
		if snapErr == nil && !snapshot.Running {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("shell operation did not complete")
}

func TestComposerSubmitMutationCompletesUnknownAndDeniesHostWriteWithoutTurn(t *testing.T) {
	_, runtime, controller, registry := newComposerRuntime(t)
	unknown := composerParams(runtime, "composer-unknown", "/unknown-command")
	result, err := runtime.ComposerSubmitMutation(context.Background(), registry, unknown, nil)
	if err != nil || result.Kind != protocol.SubmitCompleted || result.Effect != protocol.EffectNone || result.TurnID != "" {
		t.Fatalf("unknown = %+v, %v", result, err)
	}

	denied := composerParams(runtime, "composer-denied", "/hooks trust")
	result, err = runtime.ComposerSubmitMutation(context.Background(), registry, denied, nil)
	if err != nil || result.Kind != protocol.SubmitCompleted || result.Effect != protocol.EffectNone {
		t.Fatalf("denied = %+v, %v", result, err)
	}
	controller.composerMu.Lock()
	calls := append([]string(nil), controller.calls...)
	controller.composerMu.Unlock()
	if !reflect.DeepEqual(calls, []string{"display"}) {
		t.Fatalf("Controller received denied Host write: calls=%v", calls)
	}
}

func TestComposerSubmitMutationDelegatesLifecycleBeforeRequestRegistration(t *testing.T) {
	_, runtime, _, registry := newComposerRuntime(t)
	params := composerParams(runtime, "composer-new", "/new")
	_, err := runtime.ComposerSubmitMutation(context.Background(), registry, params, nil)
	var delegation *ComposerDelegationError
	if !errors.As(err, &delegation) || delegation.Route.Lifecycle != "new" {
		t.Fatalf("delegation = %v", err)
	}
	request := idempotency.Request{
		RequestID: params.RequestID, Method: string(protocol.MethodSessionSubmit),
		Target: idempotency.SessionTarget(params.Target), Params: params,
	}
	if _, found, lookupErr := registry.Lookup(request); lookupErr != nil || found {
		t.Fatalf("delegated request registered: found=%v err=%v", found, lookupErr)
	}
}

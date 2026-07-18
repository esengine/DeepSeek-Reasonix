package host

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/checkpoint"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/remote/idempotency"
	"reasonix/internal/remote/protocol"
)

func operationSessionMutation(runtime *SessionRuntime, requestID protocol.RequestID) protocol.SessionMutation {
	return protocol.SessionMutation{
		RequestID: requestID, ExpectedHostEpoch: "host-test", Target: runtime.Target(),
		ExpectedRuntimeEpoch: runtime.Epoch(),
	}
}

func waitForOperationIdle(t *testing.T, runtime *SessionRuntime) RuntimeSnapshot {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		snapshot, err := runtime.Snapshot(context.Background())
		if err != nil {
			t.Fatalf("Snapshot: %v", err)
		}
		if !snapshot.Running && snapshot.CurrentOperation == nil {
			return snapshot
		}
		if time.Now().After(deadline) {
			t.Fatalf("Operation did not finish: %+v", snapshot.CurrentOperation)
		}
		time.Sleep(time.Millisecond)
	}
}

func requireOperationEvent(t *testing.T, messages <-chan SubscriptionMessage, operationID protocol.OperationID) RuntimeEvent {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case message, ok := <-messages:
			if !ok {
				t.Fatal("subscription closed before Operation event")
			}
			if message.Event == nil {
				t.Fatalf("unexpected subscription message: %+v", message)
			}
			if message.Event.OperationID == operationID {
				if message.Event.TurnID != "" {
					t.Fatalf("Operation event also carried TurnID %q", message.Event.TurnID)
				}
				return *message.Event
			}
		case <-deadline:
			t.Fatalf("timed out waiting for Operation %q event", operationID)
		}
	}
}

func requireOperationDoneEvent(t *testing.T, messages <-chan SubscriptionMessage, operationID protocol.OperationID) RuntimeEvent {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case message, ok := <-messages:
			if !ok {
				t.Fatal("subscription closed before Operation completion")
			}
			if message.Event == nil {
				t.Fatalf("unexpected subscription message: %+v", message)
			}
			if message.Event.Event.Kind != "operation_done" {
				continue
			}
			if message.Event.OperationID != operationID || message.Event.TurnID != "" {
				t.Fatalf("Operation completion identity = turn %q operation %q, want %q", message.Event.TurnID, message.Event.OperationID, operationID)
			}
			return *message.Event
		case <-deadline:
			t.Fatalf("timed out waiting for Operation %q completion", operationID)
		}
	}
}

func TestOperationSurvivesCallerCancellationAndReconnectSnapshot(t *testing.T) {
	manager, factory := newTestRuntimeManager(t, context.Background(), 32, 64)
	runtime, err := manager.GetOrCreate(testTarget())
	if err != nil {
		t.Fatal(err)
	}
	registry := newLifecycleRegistry(t)
	firstAttachment := testAttachment(1)
	first, err := runtime.Subscribe(context.Background(), firstAttachment, "")
	if err != nil {
		t.Fatal(err)
	}

	request := protocol.ShellRunParams{
		SessionMutation: operationSessionMutation(runtime, "request-operation-start"),
		Command:         "sleep 30",
	}
	caller, cancelCaller := context.WithCancel(context.Background())
	started, err := runtime.StartShellMutation(caller, registry, request, nil)
	if err != nil {
		t.Fatal(err)
	}
	cancelCaller() // models an attach/RPC response context disappearing
	requireOperationEvent(t, first.Messages, started.OperationID)

	if err := runtime.detachAttachment(context.Background(), firstAttachment); err != nil {
		t.Fatal(err)
	}
	second, err := runtime.Subscribe(context.Background(), testAttachment(2), "")
	if err != nil {
		t.Fatal(err)
	}
	current := second.Snapshot.CurrentOperation
	if !second.Snapshot.Running || current == nil || current.OperationID != started.OperationID ||
		current.Kind != protocol.OperationShell || current.CancelRequested {
		t.Fatalf("reconnect Operation snapshot = %+v, running=%v", current, second.Snapshot.Running)
	}
	if len(second.Snapshot.Events) == 0 {
		t.Fatal("reconnect snapshot lost active Shell progress")
	}
	for _, envelope := range second.Snapshot.Events {
		if envelope.OperationID != started.OperationID || envelope.TurnID != "" {
			t.Fatalf("snapshot event identity = turn %q operation %q", envelope.TurnID, envelope.OperationID)
		}
	}

	cancelParams := protocol.OperationCancelParams{
		SessionMutation:     operationSessionMutation(runtime, "request-operation-cancel"),
		ExpectedOperationID: started.OperationID,
	}
	cancelled, err := runtime.CancelOperationMutation(context.Background(), registry, cancelParams, nil)
	if err != nil || cancelled.Status != protocol.CancelRequested || cancelled.OperationID != started.OperationID {
		t.Fatalf("CancelOperationMutation = %+v, %v", cancelled, err)
	}
	done := requireOperationDoneEvent(t, second.Messages, started.OperationID)
	if done.Event.Err == "" {
		t.Fatal("cancelled Operation completion omitted its error outcome")
	}
	finished := waitForOperationIdle(t, runtime)
	if finished.LastOutcome != protocol.OutcomeCancelled {
		t.Fatalf("completion outcome = %q, want cancelled", finished.LastOutcome)
	}

	// Both response-loss replays remain exact after live state is gone. Neither
	// retry can execute or cancel a later Operation.
	replayedStart, err := runtime.StartShellMutation(context.Background(), registry, request, nil)
	if err != nil || replayedStart != started {
		t.Fatalf("replayed start = %+v, %v; want %+v", replayedStart, err, started)
	}
	replayedCancel, err := runtime.CancelOperationMutation(context.Background(), registry, cancelParams, nil)
	if err != nil || replayedCancel != cancelled {
		t.Fatalf("replayed cancel = %+v, %v; want %+v", replayedCancel, err, cancelled)
	}
	if specs := factory.controller(0).operationSpecSnapshot(); len(specs) != 1 {
		t.Fatalf("Controller StartOperation calls = %d, want 1", len(specs))
	}
}

func TestStrictOperationCancelNowAlreadyNotActiveAndMismatch(t *testing.T) {
	manager, _ := newTestRuntimeManager(t, context.Background(), 16, 64)
	runtime, err := manager.GetOrCreate(testTarget())
	if err != nil {
		t.Fatal(err)
	}
	registry := newLifecycleRegistry(t)
	started, err := runtime.StartShellMutation(context.Background(), registry, protocol.ShellRunParams{
		SessionMutation: operationSessionMutation(runtime, "request-cancel-states-start"), Command: "sleep 30",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := runtime.CancelOperation(context.Background(), "operation-stale"); !errors.Is(err, ErrOperationMismatch) {
		t.Fatalf("mismatched CancelOperation = %v, want ErrOperationMismatch", err)
	}
	pairValue, err := runtime.call(context.Background(), func(state *runtimeActorState) (any, error) {
		first, firstErr := cancelOperationForState(state, started.OperationID)
		if firstErr != nil {
			return nil, firstErr
		}
		second, secondErr := cancelOperationForState(state, started.OperationID)
		if secondErr != nil {
			return nil, secondErr
		}
		return [2]protocol.OperationCancelResult{first, second}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	pair := pairValue.([2]protocol.OperationCancelResult)
	if pair[0].Status != protocol.CancelRequested || pair[1].Status != protocol.CancelAlreadyRequested {
		t.Fatalf("strict cancel states = %+v", pair)
	}
	waitForOperationIdle(t, runtime)
	if _, err := runtime.CancelOperation(context.Background(), started.OperationID); !errors.Is(err, ErrOperationNotActive) {
		t.Fatalf("completed CancelOperation = %v, want ErrOperationNotActive", err)
	}
}

func TestOperationMutationsMapEveryForegroundBusyAxis(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*testing.T, *SessionRuntime, *fakeSessionController, *idempotency.Registry) func()
	}{
		{
			name: "turn",
			setup: func(t *testing.T, runtime *SessionRuntime, controller *fakeSessionController, _ *idempotency.Registry) func() {
				if _, err := runtime.Submit(context.Background(), "hold Turn"); err != nil {
					t.Fatal(err)
				}
				return controller.releaseTurn
			},
		},
		{
			name: "prompt",
			setup: func(t *testing.T, runtime *SessionRuntime, controller *fakeSessionController, _ *idempotency.Registry) func() {
				controller.emit(event.Event{Kind: event.ApprovalRequest, Approval: event.Approval{
					ID: "controller-prompt", Tool: "bash", Subject: "approve",
				}})
				deadline := time.Now().Add(2 * time.Second)
				for {
					snapshot, err := runtime.Snapshot(context.Background())
					if err != nil {
						t.Fatal(err)
					}
					if snapshot.PendingPrompt != nil {
						break
					}
					if time.Now().After(deadline) {
						t.Fatal("Prompt did not reach actor")
					}
					time.Sleep(time.Millisecond)
				}
				return func() {}
			},
		},
		{
			name: "operation",
			setup: func(t *testing.T, runtime *SessionRuntime, _ *fakeSessionController, registry *idempotency.Registry) func() {
				started, err := runtime.StartShellMutation(context.Background(), registry, protocol.ShellRunParams{
					SessionMutation: operationSessionMutation(runtime, "request-busy-existing-operation"), Command: "sleep 30",
				}, nil)
				if err != nil {
					t.Fatal(err)
				}
				return func() {
					_, _ = runtime.CancelOperation(context.Background(), started.OperationID)
					waitForOperationIdle(t, runtime)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manager, factory := newTestRuntimeManager(t, context.Background(), 16, 64)
			runtime, err := manager.GetOrCreate(testTarget())
			if err != nil {
				t.Fatal(err)
			}
			registry := newLifecycleRegistry(t)
			cleanup := test.setup(t, runtime, factory.controller(0), registry)
			params := protocol.SessionCompactParams{
				SessionMutation: operationSessionMutation(runtime, protocol.RequestID("request-busy-"+test.name)),
				Instructions:    "keep facts",
			}
			_, err = runtime.StartCompactMutation(context.Background(), registry, params, nil)
			requireRemoteCode(t, err, protocol.ErrSessionBusy)
			cleanup()
			// The deterministic rejection is cached. A later idle state cannot turn
			// the same semantic request into an admitted Controller Operation.
			_, err = runtime.StartCompactMutation(context.Background(), registry, params, nil)
			requireRemoteCode(t, err, protocol.ErrSessionBusy)
			if specs := factory.controller(0).operationSpecSnapshot(); len(specs) != map[bool]int{true: 1, false: 0}[test.name == "operation"] {
				t.Fatalf("Controller Operation calls after busy rejection = %d", len(specs))
			}
		})
	}
}

func TestCompactAndSummarizeBuildExactControllerSpecs(t *testing.T) {
	t.Run("compact", func(t *testing.T) {
		manager, factory := newTestRuntimeManager(t, context.Background(), 16, 64)
		runtime, err := manager.GetOrCreate(testTarget())
		if err != nil {
			t.Fatal(err)
		}
		controller := factory.controller(0)
		controller.mu.Lock()
		controller.operationMap = func(control.OperationSpec) control.OperationSpec {
			return control.OperationSpec{Kind: control.OperationShell, Command: "sleep 30"}
		}
		controller.mu.Unlock()
		registry := newLifecycleRegistry(t)
		started, err := runtime.StartCompactMutation(context.Background(), registry, protocol.SessionCompactParams{
			SessionMutation: operationSessionMutation(runtime, "request-compact-spec"), Instructions: "  preserve decisions  ",
		}, nil)
		if err != nil {
			t.Fatal(err)
		}
		specs := controller.operationSpecSnapshot()
		if len(specs) != 1 || specs[0].Kind != control.OperationCompact || specs[0].Instructions != "preserve decisions" {
			t.Fatalf("compact Controller spec = %+v", specs)
		}
		_, _ = runtime.CancelOperation(context.Background(), started.OperationID)
		waitForOperationIdle(t, runtime)
	})

	t.Run("summarize", func(t *testing.T) {
		manager, factory := newTestRuntimeManager(t, context.Background(), 16, 64)
		runtime, err := manager.GetOrCreate(testTarget())
		if err != nil {
			t.Fatal(err)
		}
		controller := factory.controller(0)
		controller.mu.Lock()
		controller.checkpointState = control.CheckpointSnapshot{
			Metas:               []checkpoint.Meta{{Turn: 4, Time: time.UnixMilli(1_700_000_000_000)}},
			TurnsByMessageIndex: map[int]int{}, ConversationAvailable: map[int]bool{4: true},
		}
		controller.operationMap = func(control.OperationSpec) control.OperationSpec {
			return control.OperationSpec{Kind: control.OperationShell, Command: "sleep 30"}
		}
		controller.mu.Unlock()
		subscription, err := runtime.Subscribe(context.Background(), testAttachment(1), "")
		if err != nil {
			t.Fatal(err)
		}
		if len(subscription.Snapshot.Capture.Checkpoints) != 1 {
			t.Fatalf("checkpoint capture = %+v", subscription.Snapshot.Capture.Checkpoints)
		}
		checkpointID := subscription.Snapshot.Capture.Checkpoints[0].CheckpointID
		turn, err := runtime.ResolveCheckpointTurn(context.Background(), checkpointID)
		if err != nil || turn != 4 {
			t.Fatalf("ResolveCheckpointTurn = %d, %v", turn, err)
		}
		registry := newLifecycleRegistry(t)
		started, err := runtime.StartSummarizeMutation(context.Background(), registry, protocol.SessionSummarizeParams{
			SessionMutation: operationSessionMutation(runtime, "request-summarize-spec"),
			CheckpointID:    checkpointID, Direction: protocol.SummaryUpTo,
		}, nil)
		if err != nil {
			t.Fatal(err)
		}
		specs := controller.operationSpecSnapshot()
		if len(specs) != 1 || specs[0].Kind != control.OperationSummarize || specs[0].Turn != 4 || specs[0].Direction != control.SummarizeUpTo {
			t.Fatalf("summarize Controller spec = %+v", specs)
		}
		_, _ = runtime.CancelOperation(context.Background(), started.OperationID)
		waitForOperationIdle(t, runtime)

		controller.mu.Lock()
		controller.checkpointState = control.CheckpointSnapshot{
			Metas: []checkpoint.Meta{}, TurnsByMessageIndex: map[int]int{}, ConversationAvailable: map[int]bool{},
		}
		controller.mu.Unlock()
		if _, err := runtime.ResolveCheckpointTurn(context.Background(), checkpointID); !errors.Is(err, ErrCheckpointNotFound) {
			t.Fatalf("removed checkpoint resolved as %v", err)
		}
	})
}

func TestOperationReplacementEpochIsolationAndDaemonLifetimeIDNonReuse(t *testing.T) {
	factory := &fakeControllerFactory{}
	var epoch atomic.Uint64
	var operationCalls atomic.Uint64
	manager, err := NewRuntimeManager(context.Background(), "host-test", factory, RuntimeManagerOptions{
		NewRuntimeEpoch: func() (protocol.RuntimeEpoch, error) {
			return protocol.RuntimeEpoch(fmt.Sprintf("runtime-replacement-%d", epoch.Add(1))), nil
		},
		NewOperationID: func() (protocol.OperationID, error) {
			switch operationCalls.Add(1) {
			case 1, 2:
				return "operation-never-reuse", nil
			default:
				return "operation-after-replacement", nil
			}
		},
		SubscriptionQueue: 16,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	registry := newLifecycleRegistry(t)
	oldRuntime, err := manager.GetOrCreate(testTarget())
	if err != nil {
		t.Fatal(err)
	}
	oldStarted, err := oldRuntime.StartShellMutation(context.Background(), registry, protocol.ShellRunParams{
		SessionMutation: operationSessionMutation(oldRuntime, "request-old-operation"), Command: "true",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	waitForOperationIdle(t, oldRuntime)
	oldSubscription, err := oldRuntime.Subscribe(context.Background(), testAttachment(1), "")
	if err != nil {
		t.Fatal(err)
	}

	newRuntime, err := manager.Replace(testTarget())
	if err != nil {
		t.Fatal(err)
	}
	terminal := receiveMessage(t, oldSubscription.Messages)
	if terminal.Resync == nil || terminal.Resync.Reason != protocol.ResyncRuntimeReplaced {
		t.Fatalf("replacement terminal = %+v", terminal)
	}
	newStarted, err := newRuntime.StartShellMutation(context.Background(), registry, protocol.ShellRunParams{
		SessionMutation: operationSessionMutation(newRuntime, "request-new-operation"), Command: "sleep 30",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if oldStarted.OperationID != "operation-never-reuse" || newStarted.OperationID != "operation-after-replacement" {
		t.Fatalf("Operation IDs old=%q new=%q", oldStarted.OperationID, newStarted.OperationID)
	}
	if oldRuntime.Epoch() == newRuntime.Epoch() {
		t.Fatalf("replacement reused runtime epoch %q", newRuntime.Epoch())
	}
	if _, err := newRuntime.CancelOperation(context.Background(), oldStarted.OperationID); !errors.Is(err, ErrOperationMismatch) {
		t.Fatalf("stale Operation ID cancel = %v", err)
	}

	newSubscription, err := newRuntime.Subscribe(context.Background(), testAttachment(2), "")
	if err != nil {
		t.Fatal(err)
	}
	// StartOperation returns after admission, while its first Controller event is
	// independently sequenced. Wait for that exact new-runtime dispatch before
	// probing the immutable old sink; a quiet-period drain can misclassify a
	// scheduler-delayed dispatch as an event from the old runtime.
	dispatch := requireOperationEvent(t, newSubscription.Messages, newStarted.OperationID)
	if dispatch.RuntimeEpoch != newRuntime.Epoch() || dispatch.Event.Kind != "tool_dispatch" {
		t.Fatalf("new-runtime dispatch = %+v", dispatch)
	}
	factory.controller(0).emit(event.Event{Kind: event.ToolProgress, Tool: event.Tool{ID: "old", Output: "late"}})
	snapshot, err := newRuntime.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// Snapshot is an actor barrier. If the old immutable sink were accidentally
	// rebound to the replacement, the probe above would have been sequenced
	// before this capture and consumed the next replacement seq.
	if snapshot.BoundarySeq != dispatch.Seq {
		t.Fatalf("old-runtime probe advanced replacement boundary from %d to %d: %+v", dispatch.Seq, snapshot.BoundarySeq, snapshot.Events)
	}
	select {
	case message := <-newSubscription.Messages:
		if message.Event != nil {
			t.Fatalf("replacement received event after old-runtime probe: runtime=%q operation=%q seq=%d kind=%q tool=%+v", message.Event.RuntimeEpoch, message.Event.OperationID, message.Event.Seq, message.Event.Event.Kind, message.Event.Event.Tool)
		}
		t.Fatalf("replacement received non-event after old-runtime probe: %+v", message)
	default:
	}
	_, _ = newRuntime.CancelOperation(context.Background(), newStarted.OperationID)
	waitForOperationIdle(t, newRuntime)
}

func TestOperationCancelMutationTypedMismatchAndNotActiveAreCached(t *testing.T) {
	manager, _ := newTestRuntimeManager(t, context.Background(), 16, 64)
	runtime, err := manager.GetOrCreate(testTarget())
	if err != nil {
		t.Fatal(err)
	}
	registry := newLifecycleRegistry(t)
	started, err := runtime.StartShellMutation(context.Background(), registry, protocol.ShellRunParams{
		SessionMutation: operationSessionMutation(runtime, "request-typed-cancel-start"), Command: "sleep 30",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	mismatch := protocol.OperationCancelParams{
		SessionMutation:     operationSessionMutation(runtime, "request-typed-cancel-mismatch"),
		ExpectedOperationID: "operation-stale",
	}
	_, err = runtime.CancelOperationMutation(context.Background(), registry, mismatch, nil)
	requireRemoteCode(t, err, protocol.ErrOperationMismatch)
	_, _ = runtime.CancelOperation(context.Background(), started.OperationID)
	waitForOperationIdle(t, runtime)
	_, err = runtime.CancelOperationMutation(context.Background(), registry, mismatch, nil)
	requireRemoteCode(t, err, protocol.ErrOperationMismatch)

	notActive := protocol.OperationCancelParams{
		SessionMutation:     operationSessionMutation(runtime, "request-typed-cancel-not-active"),
		ExpectedOperationID: started.OperationID,
	}
	_, err = runtime.CancelOperationMutation(context.Background(), registry, notActive, nil)
	requireRemoteCode(t, err, protocol.ErrOperationNotActive)
}

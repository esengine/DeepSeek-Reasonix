package host

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/remote/idempotency"
	"reasonix/internal/remote/protocol"
)

func newActivePromptRuntime(t *testing.T) (*RuntimeManager, *SessionRuntime, *fakeSessionController, *idempotency.Registry, SubmitResult) {
	t.Helper()
	manager, factory := newTestRuntimeManager(t, context.Background(), 32, 128)
	runtime, err := manager.GetOrCreate(testTarget())
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtime.Submit(context.Background(), "prompt mutation test")
	if err != nil {
		t.Fatal(err)
	}
	registry, err := idempotency.New("host-test", idempotency.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return manager, runtime, factory.controller(0), registry, result
}

func mutationEnvelope(runtime *SessionRuntime, requestID protocol.RequestID) protocol.SessionMutation {
	return protocol.SessionMutation{
		RequestID: requestID, ExpectedHostEpoch: "host-test", Target: runtime.Target(),
		ExpectedRuntimeEpoch: runtime.Epoch(),
	}
}

func emitApprovalAndSnapshot(t *testing.T, runtime *SessionRuntime, controller *fakeSessionController, approval event.Approval) RuntimeSnapshot {
	t.Helper()
	controller.emit(event.Event{Kind: event.ApprovalRequest, Approval: approval})
	snapshot, err := runtime.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.PendingPrompt == nil || snapshot.PendingPrompt.Kind != protocol.PromptApproval || snapshot.PendingPrompt.Approval == nil {
		t.Fatalf("approval snapshot = %+v", snapshot.PendingPrompt)
	}
	return snapshot
}

func emitAskAndSnapshot(t *testing.T, runtime *SessionRuntime, controller *fakeSessionController, ask event.Ask) RuntimeSnapshot {
	t.Helper()
	controller.emit(event.Event{Kind: event.AskRequest, Ask: ask})
	snapshot, err := runtime.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.PendingPrompt == nil || snapshot.PendingPrompt.Kind != protocol.PromptAsk || snapshot.PendingPrompt.Ask == nil {
		t.Fatalf("Ask snapshot = %+v", snapshot.PendingPrompt)
	}
	return snapshot
}

func requireRemoteCode(t *testing.T, err error, code protocol.ReasonixErrorCode) {
	t.Helper()
	var remote *protocol.RemoteError
	if !errors.As(err, &remote) || remote.Code != code {
		t.Fatalf("error = %v, want RemoteError %s", err, code)
	}
}

func TestPromptEventOpaqueIdentityReconnectSnapshotAndSafetySidecar(t *testing.T) {
	manager, runtime, controller, registry, _ := newActivePromptRuntime(t)
	attachment := testAttachment(1)
	subscription, err := runtime.Subscribe(context.Background(), attachment, "")
	if err != nil {
		t.Fatal(err)
	}
	privateID := "controller-approval-private"
	approval := event.Approval{
		ID: privateID, Tool: "mcp__srv__wipe", Subject: "srv/wipe", Reason: "destructive capability",
		MCPTrust: &event.MCPTrust{
			Server: "srv", TrustState: "changed", IsolationState: "isolated", IsolationReason: "identity changed",
			IdentityChanged: true, ChangedTools: []string{"wipe"}, Readers: []string{"read"},
			Writers: []string{"wipe"}, Destructive: []string{"wipe"},
			ToolChanges: []event.MCPToolChange{{Name: "wipe", Kind: "changed"}},
		},
	}
	snapshot := emitApprovalAndSnapshot(t, runtime, controller, approval)
	if prompts := countLivePromptEvents(snapshot); prompts != 1 {
		t.Fatalf("initial semantic Prompt events = %d, want 1", prompts)
	}
	hostID := snapshot.PendingPrompt.Approval.PromptID
	if hostID == "" || string(hostID) == privateID {
		t.Fatalf("Host PromptID = %q, private=%q", hostID, privateID)
	}
	message := receiveMessage(t, subscription.Messages)
	if message.Event == nil || message.Event.Event.Approval == nil || message.Event.Event.Approval.ID != string(hostID) {
		t.Fatalf("rewritten approval event = %+v", message)
	}
	if bytes.Contains(mustJSON(t, message.Event), []byte(privateID)) {
		t.Fatal("Controller-private approval ID leaked through session event")
	}
	message.Event.Event.Approval.MCPTrust.ChangedTools[0] = "outbound-mutated"
	summary, exists := manager.SessionSummary(runtime.Target())
	if !exists || !summary.PendingPrompt {
		t.Fatalf("runtime summary = %+v, exists=%v", summary, exists)
	}

	sidecar := snapshot.PendingPromptEvent()
	if sidecar == nil || sidecar.Approval == nil || sidecar.Approval.ID != string(hostID) || sidecar.Approval.MCPTrust == nil ||
		sidecar.Approval.MCPTrust.IsolationReason != "identity changed" || len(sidecar.Approval.MCPTrust.ToolChanges) != 1 {
		t.Fatalf("Host safety sidecar = %#v", sidecar)
	}
	// Returned snapshots and sidecars are defensive copies.
	sidecar.Approval.MCPTrust.ChangedTools[0] = "mutated"
	snapshot.PendingPrompt.Approval.AllowedDecisions[0] = protocol.DecisionDeny
	again, err := runtime.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if again.PendingPromptEvent().Approval.MCPTrust.ChangedTools[0] != "wipe" ||
		again.PendingPrompt.Approval.AllowedDecisions[0] != protocol.DecisionAllowOnce {
		t.Fatal("caller mutation escaped snapshot defensive copy")
	}
	encoded := mustJSON(t, struct {
		PendingPrompt *protocol.PendingPrompt
		Snapshot      RuntimeSnapshot
	}{PendingPrompt: again.PendingPrompt, Snapshot: RuntimeSnapshot{PendingPrompt: again.PendingPrompt}})
	if bytes.Contains(encoded, []byte(privateID)) || bytes.Contains(encoded, []byte("pendingPromptEvent")) || bytes.Contains(encoded, []byte("mcpTrust")) {
		t.Fatalf("Host-only Prompt fields entered generic snapshot JSON: %s", encoded)
	}

	if err := manager.DetachAttachment(attachment); err != nil {
		t.Fatal(err)
	}
	requireClosed(t, subscription.Messages)
	reconnected, err := runtime.Subscribe(context.Background(), testAttachment(2), "")
	if err != nil {
		t.Fatal(err)
	}
	if reconnected.Snapshot.PendingPrompt == nil || reconnected.Snapshot.PendingPrompt.Approval.PromptID != hostID {
		t.Fatalf("reconnect Prompt = %+v, want %q", reconnected.Snapshot.PendingPrompt, hostID)
	}

	// Controller ReplayPendingPrompts uses its private ID again; Host must reuse
	// the current opaque ID rather than minting a second actionable identity.
	controller.emit(event.Event{Kind: event.ApprovalRequest, Approval: event.Approval{
		ID: privateID, Tool: approval.Tool, Subject: approval.Subject, Reason: approval.Reason,
	}})
	replayed, err := runtime.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if replayed.PendingPrompt.Approval.PromptID != hostID {
		t.Fatalf("replayed PromptID = %q, want %q", replayed.PendingPrompt.Approval.PromptID, hostID)
	}
	if prompts := countLivePromptEvents(replayed); prompts != 1 {
		t.Fatalf("Controller Prompt replay produced %d semantic Prompt events", prompts)
	}
	if replayMessage := receiveMessage(t, reconnected.Messages); replayMessage.Event == nil ||
		replayMessage.Event.Event.Approval == nil || replayMessage.Event.Event.Approval.ID != string(hostID) ||
		replayMessage.Event.Event.Approval.MCPTrust == nil {
		t.Fatalf("replayed event = %+v", replayMessage)
	}
	if replayed.PendingPromptEvent().Approval.MCPTrust == nil {
		t.Fatal("Controller replay dropped Host-retained MCP safety context")
	}

	params := protocol.PromptApproveParams{
		SessionMutation: mutationEnvelope(runtime, "request-opaque-approve"),
		PromptID:        hostID, Decision: protocol.DecisionDeny,
	}
	if result, err := runtime.ApproveMutation(context.Background(), registry, params, nil); err != nil || !result.Resolved || result.PromptID != hostID {
		t.Fatalf("approve result = %+v, %v", result, err)
	}
	_, approvals, _ := controller.promptMutationCalls()
	if len(approvals) != 1 || approvals[0].ID != privateID || approvals[0].Allow {
		t.Fatalf("Controller approval mapping = %+v", approvals)
	}
	resolved, err := runtime.Snapshot(context.Background())
	if err != nil || resolved.PendingPrompt != nil || countLivePromptEvents(resolved) != 0 {
		t.Fatalf("resolved Approval remained in snapshot: pending=%+v live=%+v err=%v", resolved.PendingPrompt, resolved.Events, err)
	}
}

func TestApprovalAllowedDecisionsAndControllerFlags(t *testing.T) {
	_, runtime, controller, registry, _ := newActivePromptRuntime(t)
	tests := []struct {
		name     string
		approval event.Approval
		want     []protocol.PromptDecision
		decision protocol.PromptDecision
		flags    fakeApprovalCall
	}{
		{
			name: "ordinary", approval: event.Approval{ID: "ordinary-private", Tool: "bash", Subject: "go test ./..."},
			want:     []protocol.PromptDecision{protocol.DecisionAllowOnce, protocol.DecisionAllowSession, protocol.DecisionAllowPersistent, protocol.DecisionDeny},
			decision: protocol.DecisionAllowPersistent,
			flags:    fakeApprovalCall{ID: "ordinary-private", Allow: true, Session: true, Persist: true},
		},
		{
			name: "fresh session grant", approval: event.Approval{ID: "config-private", Tool: control.ManagedConfigWriteApprovalTool, Subject: "write config", Fresh: true},
			want:     []protocol.PromptDecision{protocol.DecisionAllowOnce, protocol.DecisionAllowSession, protocol.DecisionDeny},
			decision: protocol.DecisionAllowSession,
			flags:    fakeApprovalCall{ID: "config-private", Allow: true, Session: true},
		},
		{
			name: "other fresh", approval: event.Approval{ID: "remember-private", Tool: "remember", Fresh: false},
			want:     []protocol.PromptDecision{protocol.DecisionAllowOnce, protocol.DecisionDeny},
			decision: protocol.DecisionDeny,
			flags:    fakeApprovalCall{ID: "remember-private"},
		},
	}
	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			snapshot := emitApprovalAndSnapshot(t, runtime, controller, test.approval)
			prompt := snapshot.PendingPrompt.Approval
			if !reflect.DeepEqual(prompt.AllowedDecisions, test.want) {
				t.Fatalf("allowed decisions = %v, want %v", prompt.AllowedDecisions, test.want)
			}
			if test.name == "other fresh" && (!prompt.Fresh || prompt.Subject != "remember") {
				t.Fatalf("fresh normalization/subject fallback = %+v", prompt)
			}
			if test.name == "other fresh" {
				bad := protocol.PromptApproveParams{
					SessionMutation: mutationEnvelope(runtime, "request-not-allowed"),
					PromptID:        prompt.PromptID, Decision: protocol.DecisionAllowPersistent,
				}
				_, err := runtime.ApproveMutation(context.Background(), registry, bad, nil)
				requireRemoteCode(t, err, protocol.ErrPromptDecisionNotAllowed)
				_, err = runtime.ApproveMutation(context.Background(), registry, bad, nil)
				requireRemoteCode(t, err, protocol.ErrPromptDecisionNotAllowed)
			}
			params := protocol.PromptApproveParams{
				SessionMutation: mutationEnvelope(runtime, protocol.RequestID("request-approval-"+test.name)),
				PromptID:        prompt.PromptID, Decision: test.decision,
			}
			if _, err := runtime.ApproveMutation(context.Background(), registry, params, nil); err != nil {
				t.Fatal(err)
			}
			_, calls, _ := controller.promptMutationCalls()
			if len(calls) != index+1 || calls[index] != test.flags {
				t.Fatalf("Controller calls = %+v, want call[%d]=%+v", calls, index, test.flags)
			}
		})
	}
}

func TestAskValidationKindSafetyAndSkip(t *testing.T) {
	_, runtime, controller, registry, _ := newActivePromptRuntime(t)
	ask := event.Ask{ID: "controller-ask-private", Questions: []event.AskQuestion{
		{ID: "q1", Header: "Single", Prompt: "Pick one", Options: []event.AskOption{{Label: "A"}, {Label: "B"}}},
		{ID: "q2", Header: "Multi", Prompt: "Pick several", Multi: true, Options: []event.AskOption{{Label: "X"}, {Label: "Y"}}},
	}}
	snapshot := emitAskAndSnapshot(t, runtime, controller, ask)
	promptID := snapshot.PendingPrompt.Ask.PromptID
	wrongKind := protocol.PromptApproveParams{
		SessionMutation: mutationEnvelope(runtime, "request-wrong-kind"), PromptID: promptID, Decision: protocol.DecisionDeny,
	}
	_, err := runtime.ApproveMutation(context.Background(), registry, wrongKind, nil)
	requireRemoteCode(t, err, protocol.ErrPromptKindMismatch)

	invalid := []struct {
		name    string
		answers []protocol.QuestionAnswer
	}{
		{name: "unknown question", answers: []protocol.QuestionAnswer{{QuestionID: "missing", Selected: []string{"A"}}}},
		{name: "single multiple", answers: []protocol.QuestionAnswer{{QuestionID: "q1", Selected: []string{"A", "B"}}}},
		{name: "multi custom mix", answers: []protocol.QuestionAnswer{{QuestionID: "q2", Selected: []string{"X", "custom"}}}},
		{name: "duplicate selection", answers: []protocol.QuestionAnswer{{QuestionID: "q2", Selected: []string{"X", "X"}}}},
		{name: "empty selection", answers: []protocol.QuestionAnswer{{QuestionID: "q2", Selected: []string{""}}}},
	}
	for index, test := range invalid {
		t.Run(test.name, func(t *testing.T) {
			params := protocol.PromptAnswerParams{
				SessionMutation: mutationEnvelope(runtime, protocol.RequestID("request-invalid-answer-"+string(rune('a'+index)))),
				PromptID:        promptID, Answers: test.answers,
			}
			_, err := runtime.AnswerMutation(context.Background(), registry, params, nil)
			requireRemoteCode(t, err, protocol.ErrPromptDecisionNotAllowed)
			_, err = runtime.AnswerMutation(context.Background(), registry, params, nil)
			requireRemoteCode(t, err, protocol.ErrPromptDecisionNotAllowed)
			current, snapshotErr := runtime.Snapshot(context.Background())
			if snapshotErr != nil || current.PendingPrompt == nil || current.PendingPrompt.Ask.PromptID != promptID {
				t.Fatalf("invalid answer consumed Prompt: %+v, %v", current.PendingPrompt, snapshotErr)
			}
		})
	}

	cachedInvalid := protocol.PromptAnswerParams{
		SessionMutation: mutationEnvelope(runtime, "request-invalid-answer-state-change"),
		PromptID:        promptID,
		Answers:         []protocol.QuestionAnswer{{QuestionID: "missing", Selected: []string{"A"}}},
	}
	_, err = runtime.AnswerMutation(context.Background(), registry, cachedInvalid, nil)
	requireRemoteCode(t, err, protocol.ErrPromptDecisionNotAllowed)

	valid := protocol.PromptAnswerParams{
		SessionMutation: mutationEnvelope(runtime, "request-valid-answer"), PromptID: promptID,
		Answers: []protocol.QuestionAnswer{
			{QuestionID: "q1", Selected: []string{"typed custom answer"}},
			{QuestionID: "q2", Selected: []string{"X", "Y"}},
		},
	}
	result, err := runtime.AnswerMutation(context.Background(), registry, valid, nil)
	if err != nil || !result.Resolved || result.PromptID != promptID {
		t.Fatalf("valid answer = %+v, %v", result, err)
	}
	_, _, answers := controller.promptMutationCalls()
	wantAnswers := []event.AskAnswer{{QuestionID: "q1", Selected: []string{"typed custom answer"}}, {QuestionID: "q2", Selected: []string{"X", "Y"}}}
	if len(answers) != 1 || answers[0].ID != ask.ID || !reflect.DeepEqual(answers[0].Answers, wantAnswers) {
		t.Fatalf("Controller answers = %+v", answers)
	}
	resolvedAsk, snapshotErr := runtime.Snapshot(context.Background())
	if snapshotErr != nil || resolvedAsk.PendingPrompt != nil || countLivePromptEvents(resolvedAsk) != 0 {
		t.Fatalf("resolved Ask remained in snapshot: pending=%+v live=%+v err=%v", resolvedAsk.PendingPrompt, resolvedAsk.Events, snapshotErr)
	}
	// The invalid answer was a deterministic decision about this exact pending
	// Ask. Resolving the Prompt later must not change the same requestId into a
	// PROMPT_NOT_PENDING result.
	_, err = runtime.AnswerMutation(context.Background(), registry, cachedInvalid, nil)
	requireRemoteCode(t, err, protocol.ErrPromptDecisionNotAllowed)
	if _, err := runtime.AnswerMutation(context.Background(), registry, protocol.PromptAnswerParams{
		SessionMutation: mutationEnvelope(runtime, "request-late-answer"), PromptID: promptID, Answers: valid.Answers,
	}, nil); err == nil {
		t.Fatal("late Answer unexpectedly resolved")
	} else {
		requireRemoteCode(t, err, protocol.ErrPromptNotPending)
	}

	second := emitAskAndSnapshot(t, runtime, controller, event.Ask{ID: "controller-ask-skip", Questions: ask.Questions})
	if _, err := runtime.AnswerMutation(context.Background(), registry, protocol.PromptAnswerParams{
		SessionMutation: mutationEnvelope(runtime, "request-skip-answer"), PromptID: second.PendingPrompt.Ask.PromptID,
		Answers: []protocol.QuestionAnswer{},
	}, nil); err != nil {
		t.Fatalf("empty Ask answers (Skip) = %v", err)
	}
	_, _, answers = controller.promptMutationCalls()
	if len(answers) != 2 || answers[1].ID != "controller-ask-skip" || len(answers[1].Answers) != 0 {
		t.Fatalf("Controller Skip answers = %+v", answers)
	}
}

func TestPromptMutationBeforeBeginAdmissionGuard(t *testing.T) {
	type fixture struct {
		runtime  *SessionRuntime
		registry *idempotency.Registry
		invoke   func(context.Context, func() error) error
		calls    func() int
	}
	tests := []struct {
		name    string
		prepare func(*testing.T) fixture
	}{
		{
			name: "steer",
			prepare: func(t *testing.T) fixture {
				_, runtime, controller, registry, submitted := newActivePromptRuntime(t)
				params := protocol.TurnSteerParams{
					SessionMutation: mutationEnvelope(runtime, "request-guard-steer"),
					ExpectedTurnID:  submitted.TurnID,
					Text:            "guarded steer",
				}
				return fixture{
					runtime: runtime, registry: registry,
					invoke: func(ctx context.Context, beforeBegin func() error) error {
						result, err := runtime.SteerMutation(ctx, registry, params, beforeBegin)
						if err == nil && (!result.Accepted || result.TurnID != submitted.TurnID) {
							return errors.New("guarded Steer returned an invalid success")
						}
						return err
					},
					calls: func() int {
						steers, _, _ := controller.promptMutationCalls()
						return len(steers)
					},
				}
			},
		},
		{
			name: "approve",
			prepare: func(t *testing.T) fixture {
				_, runtime, controller, registry, _ := newActivePromptRuntime(t)
				snapshot := emitApprovalAndSnapshot(t, runtime, controller, event.Approval{
					ID: "private-guard-approval", Tool: "bash", Subject: "guard approval",
				})
				promptID := snapshot.PendingPrompt.Approval.PromptID
				params := protocol.PromptApproveParams{
					SessionMutation: mutationEnvelope(runtime, "request-guard-approve"),
					PromptID:        promptID,
					Decision:        protocol.DecisionAllowOnce,
				}
				return fixture{
					runtime: runtime, registry: registry,
					invoke: func(ctx context.Context, beforeBegin func() error) error {
						result, err := runtime.ApproveMutation(ctx, registry, params, beforeBegin)
						if err == nil && (!result.Resolved || result.PromptID != promptID) {
							return errors.New("guarded Approval returned an invalid success")
						}
						return err
					},
					calls: func() int {
						_, approvals, _ := controller.promptMutationCalls()
						return len(approvals)
					},
				}
			},
		},
		{
			name: "answer",
			prepare: func(t *testing.T) fixture {
				_, runtime, controller, registry, _ := newActivePromptRuntime(t)
				snapshot := emitAskAndSnapshot(t, runtime, controller, event.Ask{
					ID: "private-guard-answer", Questions: []event.AskQuestion{{
						ID: "q1", Prompt: "Pick", Options: []event.AskOption{{Label: "A"}, {Label: "B"}},
					}},
				})
				promptID := snapshot.PendingPrompt.Ask.PromptID
				params := protocol.PromptAnswerParams{
					SessionMutation: mutationEnvelope(runtime, "request-guard-answer"),
					PromptID:        promptID,
					Answers:         []protocol.QuestionAnswer{{QuestionID: "q1", Selected: []string{"A"}}},
				}
				return fixture{
					runtime: runtime, registry: registry,
					invoke: func(ctx context.Context, beforeBegin func() error) error {
						result, err := runtime.AnswerMutation(ctx, registry, params, beforeBegin)
						if err == nil && (!result.Resolved || result.PromptID != promptID) {
							return errors.New("guarded Answer returned an invalid success")
						}
						return err
					},
					calls: func() int {
						_, _, answers := controller.promptMutationCalls()
						return len(answers)
					},
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name+"/queued failure is pre-admission", func(t *testing.T) {
			fixture := test.prepare(t)
			unblock := blockRuntimeActor(t, fixture.runtime)
			guardErr := errors.New("stale transport generation")
			var guardCalls atomic.Int32
			result := make(chan error, 1)
			go func() {
				result <- fixture.invoke(context.Background(), func() error {
					guardCalls.Add(1)
					return guardErr
				})
			}()
			waitRuntimeMailboxQueued(t, fixture.runtime)
			unblock()
			if err := <-result; !errors.Is(err, guardErr) {
				t.Fatalf("guarded mutation error = %v, want %v", err, guardErr)
			}
			if guardCalls.Load() != 1 {
				t.Fatalf("beforeBegin calls = %d, want 1", guardCalls.Load())
			}
			if stats := fixture.registry.Stats(); stats.Entries != 0 || stats.Pending != 0 || stats.Completed != 0 {
				t.Fatalf("guard failure created requestId state: %+v", stats)
			}
			if calls := fixture.calls(); calls != 0 {
				t.Fatalf("guard failure reached Controller %d times", calls)
			}
			var recoveredGuardCalls atomic.Int32
			if err := fixture.invoke(context.Background(), func() error {
				recoveredGuardCalls.Add(1)
				return nil
			}); err != nil {
				t.Fatalf("same requestId after guard recovery = %v", err)
			}
			if recoveredGuardCalls.Load() != 1 {
				t.Fatalf("recovered beforeBegin calls = %d, want 1", recoveredGuardCalls.Load())
			}
			if calls := fixture.calls(); calls != 1 {
				t.Fatalf("recovered mutation reached Controller %d times, want 1", calls)
			}
		})

		t.Run(test.name+"/successful check linearizes admission", func(t *testing.T) {
			fixture := test.prepare(t)
			var authorized atomic.Bool
			authorized.Store(true)
			checked := make(chan bool, 1)
			continueAfterCheck := make(chan struct{})
			result := make(chan error, 1)
			guardErr := errors.New("authorization changed after validation")
			go func() {
				result <- fixture.invoke(context.Background(), func() error {
					allowedAtLinearization := authorized.Load()
					checked <- allowedAtLinearization
					<-continueAfterCheck
					if !allowedAtLinearization {
						return guardErr
					}
					return nil
				})
			}()
			if allowed := <-checked; !allowed {
				t.Fatal("beforeBegin did not observe the initial authorization")
			}
			if calls := fixture.calls(); calls != 0 {
				t.Fatalf("Controller was called before beforeBegin returned: %d", calls)
			}
			authorized.Store(false)
			close(continueAfterCheck)
			if err := <-result; err != nil {
				t.Fatalf("mutation admitted before external state change = %v", err)
			}
			if calls := fixture.calls(); calls != 1 {
				t.Fatalf("admitted mutation reached Controller %d times, want 1", calls)
			}
			if stats := fixture.registry.Stats(); stats.Entries != 1 || stats.Pending != 0 || stats.Completed != 1 {
				t.Fatalf("admitted requestId state = %+v", stats)
			}
			if err := fixture.invoke(context.Background(), nil); err != nil {
				t.Fatalf("admitted mutation replay = %v", err)
			}
			if calls := fixture.calls(); calls != 1 {
				t.Fatalf("replay repeated Controller call: %d", calls)
			}
		})
	}
}

func TestTargetRemovalReservationDoesNotDeadlockQueuedPromptGuard(t *testing.T) {
	manager, runtime, controller, registry, _ := newActivePromptRuntime(t)
	snapshot := emitAskAndSnapshot(t, runtime, controller, event.Ask{
		ID: "private-removal-race",
		Questions: []event.AskQuestion{{
			ID: "q1", Prompt: "Choose", Options: []event.AskOption{{Label: "A"}, {Label: "B"}},
		}},
	})
	params := protocol.PromptAnswerParams{
		SessionMutation: mutationEnvelope(runtime, "request-removal-race"),
		PromptID:        snapshot.PendingPrompt.Ask.PromptID,
		Answers:         []protocol.QuestionAnswer{{QuestionID: "q1", Selected: []string{"A"}}},
	}

	guardEntered := make(chan struct{})
	releaseGuard := make(chan struct{})
	mutationDone := make(chan error, 1)
	go func() {
		_, err := runtime.AnswerMutation(context.Background(), registry, params, func() error {
			close(guardEntered)
			<-releaseGuard
			return nil
		})
		mutationDone <- err
	}()
	select {
	case <-guardEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("queued Prompt guard did not enter")
	}

	reservationDone := make(chan error, 1)
	go func() {
		reservation, err := manager.ReserveTargetsForRemoval([]protocol.RuntimeTarget{runtime.Target()})
		if err == nil {
			reservation.Abort()
		}
		reservationDone <- err
	}()
	deadline := time.Now().Add(2 * time.Second)
	for runtime.current.Load() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if runtime.current.Load() {
		close(releaseGuard)
		t.Fatal("target-removal reservation did not retire the runtime")
	}

	// The reservation owns RuntimeManager's write lock while it waits for this
	// actor. Releasing a guard that consults RuntimeManager would deadlock here.
	close(releaseGuard)
	select {
	case err := <-mutationDone:
		requireRemoteCode(t, err, protocol.ErrStaleRuntimeEpoch)
		var remoteErr *protocol.RemoteError
		if !errors.As(err, &remoteErr) || remoteErr.Data.Target == nil ||
			remoteErr.Data.Expected != "" || remoteErr.Data.Actual != "" {
			t.Fatalf("retired runtime error = %+v", remoteErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("queued Prompt mutation deadlocked behind target-removal reservation")
	}
	select {
	case err := <-reservationDone:
		if err != nil {
			t.Fatalf("target-removal reservation = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("target-removal reservation did not finish after Prompt rejection")
	}
	if stats := registry.Stats(); stats.Entries != 0 || stats.Pending != 0 || stats.Completed != 0 {
		t.Fatalf("retired Prompt mutation created requestId state: %+v", stats)
	}
	if _, _, answers := controller.promptMutationCalls(); len(answers) != 0 {
		t.Fatalf("retired Prompt mutation reached Controller: %+v", answers)
	}
}

func TestPromptIDsNeverReuseAndLateOldIDCannotHitReplacement(t *testing.T) {
	factory := &fakeControllerFactory{}
	ids := &testIDSource{}
	var promptCalls atomic.Int32
	manager, err := NewRuntimeManager(context.Background(), "host-test", factory, RuntimeManagerOptions{
		NewRuntimeEpoch: ids.runtimeEpoch,
		NewTurnID:       ids.turnID,
		NewPromptID: func() (protocol.PromptID, error) {
			switch promptCalls.Add(1) {
			case 1, 2:
				return "prompt-issued-once", nil
			default:
				return "prompt-new", nil
			}
		},
		NewSubscriptionID: ids.subscriptionID, SubscriptionQueue: 16, EventLogLimit: 64,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	runtime, err := manager.GetOrCreate(testTarget())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Submit(context.Background(), "prompt collision"); err != nil {
		t.Fatal(err)
	}
	controller := factory.controller(0)
	registry, _ := idempotency.New("host-test", idempotency.Options{})
	first := emitApprovalAndSnapshot(t, runtime, controller, event.Approval{ID: "controller-reused", Tool: "bash", Subject: "first"})
	firstID := first.PendingPrompt.Approval.PromptID
	if firstID != "prompt-issued-once" {
		t.Fatalf("first PromptID = %q", firstID)
	}
	if _, err := runtime.ApproveMutation(context.Background(), registry, protocol.PromptApproveParams{
		SessionMutation: mutationEnvelope(runtime, "request-first-prompt"), PromptID: firstID, Decision: protocol.DecisionAllowOnce,
	}, nil); err != nil {
		t.Fatal(err)
	}
	secondEvent := event.Approval{ID: "controller-reused", Tool: "bash", Subject: "second"}
	second := emitApprovalAndSnapshot(t, runtime, controller, secondEvent)
	secondID := second.PendingPrompt.Approval.PromptID
	if secondID != "prompt-new" || secondID == firstID || promptCalls.Load() != 3 {
		t.Fatalf("second PromptID = %q, generator calls=%d", secondID, promptCalls.Load())
	}
	controller.emit(event.Event{Kind: event.ApprovalRequest, Approval: secondEvent})
	replay, err := runtime.Snapshot(context.Background())
	if err != nil || replay.PendingPrompt.Approval.PromptID != secondID || promptCalls.Load() != 3 {
		t.Fatalf("pending replay minted an ID: %+v, %v, calls=%d", replay.PendingPrompt, err, promptCalls.Load())
	}
	if _, err := runtime.ApproveMutation(context.Background(), registry, protocol.PromptApproveParams{
		SessionMutation: mutationEnvelope(runtime, "request-late-old-prompt"), PromptID: firstID, Decision: protocol.DecisionDeny,
	}, nil); err == nil {
		t.Fatal("late old PromptID hit replacement")
	} else {
		requireRemoteCode(t, err, protocol.ErrPromptNotPending)
	}
	if _, err := runtime.ApproveMutation(context.Background(), registry, protocol.PromptApproveParams{
		SessionMutation: mutationEnvelope(runtime, "request-second-prompt"), PromptID: secondID, Decision: protocol.DecisionDeny,
	}, nil); err != nil {
		t.Fatal(err)
	}
	_, approvals, _ := controller.promptMutationCalls()
	if len(approvals) != 2 || approvals[0].ID != "controller-reused" || approvals[1].ID != "controller-reused" {
		t.Fatalf("Controller private mapping calls = %+v", approvals)
	}
}

func TestStrictSteerErrorsAndIdempotentAdmission(t *testing.T) {
	manager, factory := newTestRuntimeManager(t, context.Background(), 16, 64)
	runtime, err := manager.GetOrCreate(testTarget())
	if err != nil {
		t.Fatal(err)
	}
	controller := factory.controller(0)
	registry, _ := idempotency.New("host-test", idempotency.Options{})
	noTurn := protocol.TurnSteerParams{
		SessionMutation: mutationEnvelope(runtime, "request-no-turn-steer"), ExpectedTurnID: "turn-none", Text: "late",
	}
	_, err = runtime.SteerMutation(context.Background(), registry, noTurn, nil)
	requireRemoteCode(t, err, protocol.ErrTurnNotActive)
	submitted, err := runtime.Submit(context.Background(), "active steer")
	if err != nil {
		t.Fatal(err)
	}
	_, err = runtime.SteerMutation(context.Background(), registry, noTurn, nil)
	requireRemoteCode(t, err, protocol.ErrTurnNotActive)

	wrong := protocol.TurnSteerParams{
		SessionMutation: mutationEnvelope(runtime, "request-wrong-turn-steer"), ExpectedTurnID: "turn-forged", Text: "wrong",
	}
	_, err = runtime.SteerMutation(context.Background(), registry, wrong, nil)
	requireRemoteCode(t, err, protocol.ErrTurnMismatch)
	valid := protocol.TurnSteerParams{
		SessionMutation: mutationEnvelope(runtime, "request-valid-steer"), ExpectedTurnID: submitted.TurnID, Text: "use smaller steps",
	}
	first, err := runtime.SteerMutation(context.Background(), registry, valid, nil)
	if err != nil || !first.Accepted || first.TurnID != submitted.TurnID {
		t.Fatalf("valid Steer = %+v, %v", first, err)
	}
	replay, err := runtime.SteerMutation(context.Background(), registry, valid, nil)
	if err != nil || replay != first {
		t.Fatalf("Steer replay = %+v, %v", replay, err)
	}
	controller.mu.Lock()
	controller.steerAccepted = false
	controller.mu.Unlock()
	rejected := valid
	rejected.RequestID = "request-controller-rejected-steer"
	_, err = runtime.SteerMutation(context.Background(), registry, rejected, nil)
	requireRemoteCode(t, err, protocol.ErrTurnNotActive)
	_, err = runtime.SteerMutation(context.Background(), registry, rejected, nil)
	requireRemoteCode(t, err, protocol.ErrTurnNotActive)
	steers, _, _ := controller.promptMutationCalls()
	if !reflect.DeepEqual(steers, []string{"use smaller steps"}) {
		t.Fatalf("strict Controller Steers = %v", steers)
	}
	controller.mu.Lock()
	trySteerCalls := controller.trySteerCalls
	controller.mu.Unlock()
	if trySteerCalls != 2 {
		t.Fatalf("Controller TrySteer calls = %d, want one success plus one cached rejection", trySteerCalls)
	}
}

func TestStrictSteerRejectsUnsafeOpaqueIDWithoutPanicking(t *testing.T) {
	_, runtime, _, registry, _ := newActivePromptRuntime(t)
	params := protocol.TurnSteerParams{
		SessionMutation: mutationEnvelope(runtime, "request-unsafe-turn-steer"),
		ExpectedTurnID:  "../../not-a-diagnostic-token",
		Text:            "must remain a rejection",
	}
	_, err := runtime.SteerMutation(context.Background(), registry, params, nil)
	requireRemoteCode(t, err, protocol.ErrTurnMismatch)
	var remoteErr *protocol.RemoteError
	if !errors.As(err, &remoteErr) {
		t.Fatalf("Steer error = %v", err)
	}
	if remoteErr.Data.Expected != "" || remoteErr.Data.Actual != "" {
		t.Fatalf("unsafe opaque ID leaked into diagnostics: %+v", remoteErr.Data)
	}
}

func TestMutationResponseLossRetriesDoNotRepeatControllerCalls(t *testing.T) {
	t.Run("steer", func(t *testing.T) {
		_, runtime, controller, registry, submitted := newActivePromptRuntime(t)
		entered, release := make(chan struct{}, 1), make(chan struct{})
		controller.mu.Lock()
		controller.steerEntered, controller.steerRelease = entered, release
		controller.mu.Unlock()
		params := protocol.TurnSteerParams{
			SessionMutation: mutationEnvelope(runtime, "request-lost-steer"), ExpectedTurnID: submitted.TurnID, Text: "lost response steer",
		}
		first := runCancelledSteer(t, runtime, registry, params, entered)
		if !errors.Is(first, context.Canceled) {
			t.Fatalf("first Steer error = %v", first)
		}
		close(release)
		if result, err := runtime.SteerMutation(context.Background(), registry, params, nil); err != nil || !result.Accepted {
			t.Fatalf("Steer retry = %+v, %v", result, err)
		}
		steers, _, _ := controller.promptMutationCalls()
		if len(steers) != 1 {
			t.Fatalf("Controller TrySteer calls = %d", len(steers))
		}
	})

	t.Run("approval", func(t *testing.T) {
		_, runtime, controller, registry, _ := newActivePromptRuntime(t)
		snapshot := emitApprovalAndSnapshot(t, runtime, controller, event.Approval{ID: "private-lost-approval", Tool: "bash", Subject: "go test"})
		entered, release := make(chan struct{}, 1), make(chan struct{})
		controller.mu.Lock()
		controller.approveEntered, controller.approveRelease = entered, release
		controller.mu.Unlock()
		params := protocol.PromptApproveParams{
			SessionMutation: mutationEnvelope(runtime, "request-lost-approval"),
			PromptID:        snapshot.PendingPrompt.Approval.PromptID, Decision: protocol.DecisionAllowOnce,
		}
		ctx, cancel := context.WithCancel(context.Background())
		result := make(chan error, 1)
		go func() { _, err := runtime.ApproveMutation(ctx, registry, params, nil); result <- err }()
		<-entered
		cancel()
		if err := <-result; !errors.Is(err, context.Canceled) {
			t.Fatalf("first Approve error = %v", err)
		}
		close(release)
		if replay, err := runtime.ApproveMutation(context.Background(), registry, params, nil); err != nil || !replay.Resolved {
			t.Fatalf("Approve retry = %+v, %v", replay, err)
		}
		_, calls, _ := controller.promptMutationCalls()
		if len(calls) != 1 {
			t.Fatalf("Controller Approve calls = %d", len(calls))
		}
	})

	t.Run("answer", func(t *testing.T) {
		_, runtime, controller, registry, _ := newActivePromptRuntime(t)
		snapshot := emitAskAndSnapshot(t, runtime, controller, event.Ask{ID: "private-lost-answer", Questions: []event.AskQuestion{{
			ID: "q1", Prompt: "Pick", Options: []event.AskOption{{Label: "A"}, {Label: "B"}},
		}}})
		entered, release := make(chan struct{}, 1), make(chan struct{})
		controller.mu.Lock()
		controller.answerEntered, controller.answerRelease = entered, release
		controller.mu.Unlock()
		params := protocol.PromptAnswerParams{
			SessionMutation: mutationEnvelope(runtime, "request-lost-answer"), PromptID: snapshot.PendingPrompt.Ask.PromptID,
			Answers: []protocol.QuestionAnswer{{QuestionID: "q1", Selected: []string{"A"}}},
		}
		ctx, cancel := context.WithCancel(context.Background())
		result := make(chan error, 1)
		go func() { _, err := runtime.AnswerMutation(ctx, registry, params, nil); result <- err }()
		<-entered
		cancel()
		if err := <-result; !errors.Is(err, context.Canceled) {
			t.Fatalf("first Answer error = %v", err)
		}
		close(release)
		if replay, err := runtime.AnswerMutation(context.Background(), registry, params, nil); err != nil || !replay.Resolved {
			t.Fatalf("Answer retry = %+v, %v", replay, err)
		}
		_, _, calls := controller.promptMutationCalls()
		if len(calls) != 1 {
			t.Fatalf("Controller Answer calls = %d", len(calls))
		}
	})
}

func runCancelledSteer(
	t *testing.T,
	runtime *SessionRuntime,
	registry *idempotency.Registry,
	params protocol.TurnSteerParams,
	entered <-chan struct{},
) error {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { _, err := runtime.SteerMutation(ctx, registry, params, nil); result <- err }()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("Controller Steer was not entered")
	}
	cancel()
	return <-result
}

func TestPendingPromptInvalidatedByCancelTurnDoneAndRuntimeReplacement(t *testing.T) {
	t.Run("cancel", func(t *testing.T) {
		_, runtime, controller, registry, submitted := newActivePromptRuntime(t)
		snapshot := emitApprovalAndSnapshot(t, runtime, controller, event.Approval{ID: "private-cancel", Tool: "bash", Subject: "cancel me"})
		promptID := snapshot.PendingPrompt.Approval.PromptID
		cancel := protocol.TurnCancelParams{
			SessionMutation: mutationEnvelope(runtime, "request-cancel-prompt"), ExpectedTurnID: submitted.TurnID,
		}
		if _, err := runtime.CancelTurnMutation(context.Background(), registry, cancel, nil); err != nil {
			t.Fatal(err)
		}
		current, err := runtime.Snapshot(context.Background())
		if err != nil || current.PendingPrompt != nil {
			t.Fatalf("Cancel retained Prompt = %+v, %v", current.PendingPrompt, err)
		}
		if prompts := countLivePromptEvents(current); prompts != 0 {
			t.Fatalf("Cancel retained %d semantic Prompt events", prompts)
		}
		_, err = runtime.ApproveMutation(context.Background(), registry, protocol.PromptApproveParams{
			SessionMutation: mutationEnvelope(runtime, "request-late-after-cancel"), PromptID: promptID, Decision: protocol.DecisionDeny,
		}, nil)
		requireRemoteCode(t, err, protocol.ErrPromptNotPending)
		_, approvals, _ := controller.promptMutationCalls()
		if len(approvals) != 0 {
			t.Fatalf("Cancel auto-answered Controller Prompt: %+v", approvals)
		}
	})

	t.Run("turn done", func(t *testing.T) {
		_, runtime, controller, _, _ := newActivePromptRuntime(t)
		emitApprovalAndSnapshot(t, runtime, controller, event.Approval{ID: "private-turn-done", Tool: "bash", Subject: "finish"})
		controller.releaseTurn()
		select {
		case <-controller.finished:
		case <-time.After(2 * time.Second):
			t.Fatal("turn did not finish")
		}
		current, err := runtime.Snapshot(context.Background())
		if err != nil || current.PendingPrompt != nil || current.Running {
			t.Fatalf("TurnDone state = %+v, %v", current, err)
		}
		if len(current.Events) != 0 {
			t.Fatalf("TurnDone retained live semantic projection: %+v", current.Events)
		}
		_, approvals, _ := controller.promptMutationCalls()
		if len(approvals) != 0 {
			t.Fatalf("TurnDone auto-answered Controller Prompt: %+v", approvals)
		}
	})

	t.Run("runtime replacement", func(t *testing.T) {
		manager, runtime, controller, registry, _ := newActivePromptRuntime(t)
		old := emitApprovalAndSnapshot(t, runtime, controller, event.Approval{ID: "private-replace", Tool: "bash", Subject: "old"})
		oldID := old.PendingPrompt.Approval.PromptID
		replacement, err := manager.Replace(runtime.Target())
		if err != nil {
			t.Fatal(err)
		}
		if _, approvals, _ := controller.promptMutationCalls(); len(approvals) != 0 {
			t.Fatalf("Controller Close auto-answered Prompt: %+v", approvals)
		}
		if snapshot, err := replacement.Snapshot(context.Background()); err != nil || snapshot.PendingPrompt != nil || countLivePromptEvents(snapshot) != 0 {
			t.Fatalf("replacement inherited Prompt = %+v, %v", snapshot.PendingPrompt, err)
		}
		_, err = replacement.ApproveMutation(context.Background(), registry, protocol.PromptApproveParams{
			SessionMutation: protocol.SessionMutation{
				RequestID: "request-old-epoch-prompt", ExpectedHostEpoch: "host-test", Target: replacement.Target(),
				ExpectedRuntimeEpoch: runtime.Epoch(),
			},
			PromptID: oldID, Decision: protocol.DecisionDeny,
		}, nil)
		requireRemoteCode(t, err, protocol.ErrStaleRuntimeEpoch)
		_, err = replacement.ApproveMutation(context.Background(), registry, protocol.PromptApproveParams{
			SessionMutation: mutationEnvelope(replacement, "request-old-id-new-epoch"), PromptID: oldID, Decision: protocol.DecisionDeny,
		}, nil)
		requireRemoteCode(t, err, protocol.ErrPromptNotPending)
	})
}

func blockRuntimeActor(t *testing.T, runtime *SessionRuntime) func() {
	t.Helper()
	entered := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	if !runtime.mailbox.enqueue(func(*runtimeActorState) {
		close(entered)
		<-release
	}) {
		t.Fatal("failed to enqueue actor blocker")
	}
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("actor blocker did not start")
	}
	return unblock
}

func waitRuntimeMailboxQueued(t *testing.T, runtime *SessionRuntime) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		runtime.mailbox.mu.Lock()
		queued := len(runtime.mailbox.queue)
		closed := runtime.mailbox.closed
		runtime.mailbox.mu.Unlock()
		if queued > 0 {
			return
		}
		if closed {
			t.Fatal("runtime mailbox closed before guarded mutation queued")
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("guarded mutation did not queue behind actor blocker")
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

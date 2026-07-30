package client

import (
	"context"
	"strings"
	"testing"
	"time"

	"reasonix/internal/eventwire"
	"reasonix/internal/remote/protocol"
)

func TestAuthorizeParamsFillsTypedAuthorityFields(t *testing.T) {
	c := &Client{state: State{
		Initialized: true, HostEpoch: "host-live", WorkspaceID: "workspace-live",
		Target:       protocol.RuntimeTarget{WorkspaceID: "workspace-live", SessionID: "session-live"},
		RuntimeEpoch: "runtime-live", SnapshotID: "snapshot-live", CurrentTurnID: "turn-live",
	}}

	listValue, err := c.authorizeParams(protocol.MethodSessionList, protocol.SessionListParams{})
	if err != nil {
		t.Fatalf("authorize typed session/list: %v", err)
	}
	list := listValue.(protocol.SessionListParams)
	if list.ExpectedHostEpoch != "host-live" || list.WorkspaceID != "workspace-live" {
		t.Fatalf("session/list authority = %+v", list)
	}

	submitValue, err := c.authorizeParams(protocol.MethodSessionSubmit, protocol.SessionSubmitParams{Input: "hello", DisplayText: "hello"})
	if err != nil {
		t.Fatalf("authorize typed session/submit: %v", err)
	}
	submit := submitValue.(protocol.SessionSubmitParams)
	if submit.ExpectedHostEpoch != "host-live" || submit.Target.SessionID != "session-live" || submit.ExpectedRuntimeEpoch != "runtime-live" {
		t.Fatalf("session/submit authority = %+v", submit.SessionMutation)
	}
	if !strings.HasPrefix(string(submit.RequestID), "request_") {
		t.Fatalf("session/submit requestId = %q", submit.RequestID)
	}
}

func TestAuthorizeParamsPreservesRequestIDButOverridesSpoofedAuthority(t *testing.T) {
	c := &Client{state: State{
		HostEpoch: "host-live", WorkspaceID: "workspace-live",
		Target:       protocol.RuntimeTarget{WorkspaceID: "workspace-live", SessionID: "session-live"},
		RuntimeEpoch: "runtime-live",
	}}
	value, err := c.authorizeParams(protocol.MethodSessionSubmit, protocol.SessionSubmitParams{
		SessionMutation: protocol.SessionMutation{
			RequestID: "request-stable", ExpectedHostEpoch: "host-spoofed",
			Target:               protocol.RuntimeTarget{WorkspaceID: "workspace-spoofed", SessionID: "session-spoofed"},
			ExpectedRuntimeEpoch: "runtime-spoofed",
		},
		Input: "hello", DisplayText: "hello",
	})
	if err != nil {
		t.Fatal(err)
	}
	got := value.(protocol.SessionSubmitParams)
	if got.RequestID != "request-stable" {
		t.Fatalf("requestId = %q, want stable caller retry id", got.RequestID)
	}
	if got.ExpectedHostEpoch != "host-live" || got.Target != c.state.Target || got.ExpectedRuntimeEpoch != "runtime-live" {
		t.Fatalf("spoofed authority was not replaced: %+v", got.SessionMutation)
	}
}

func TestAuthorizeSubscribeUsesCurrentReplacement(t *testing.T) {
	target := protocol.RuntimeTarget{WorkspaceID: "workspace-live", SessionID: "session-live"}
	c := &Client{state: State{
		HostEpoch: "host-live", WorkspaceID: target.WorkspaceID, Target: target,
		RuntimeEpoch: "runtime-live", SubscriptionID: "subscription-current",
	}}
	value, err := c.authorizeParams(protocol.MethodSessionSubscribe, protocol.SessionSubscribeParams{
		PageTurns: protocol.HistoryMaxTurns, ReplaceSubscriptionID: "subscription-stale",
	})
	if err != nil {
		t.Fatal(err)
	}
	got := value.(protocol.SessionSubscribeParams)
	if got.ReplaceSubscriptionID != "subscription-current" {
		t.Fatalf("replacement subscription = %q, want current client subscription", got.ReplaceSubscriptionID)
	}
}

func TestApplyResultAdoptsSessionRotationEpoch(t *testing.T) {
	target := protocol.RuntimeTarget{WorkspaceID: "workspace-live", SessionID: "session-live"}
	tests := []struct {
		name   string
		result any
		epoch  protocol.RuntimeEpoch
	}{
		{
			name: "new session",
			result: protocol.SessionNewResult{
				SourceTarget: target, Target: target, RuntimeEpoch: "runtime-new", Disposition: "created", SnapshotRequired: true,
			},
			epoch: "runtime-new",
		},
		{
			name: "clear session",
			result: protocol.SessionClearResult{
				PreviousTarget: target, Target: target, RuntimeEpoch: "runtime-cleared", Disposition: protocol.SessionCleared, SnapshotRequired: true,
			},
			epoch: "runtime-cleared",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Client{state: State{
				Initialized: true, HostEpoch: "host-live", WorkspaceID: target.WorkspaceID,
				Target: target, RuntimeEpoch: "runtime-old", SnapshotID: "snapshot-old", CurrentTurnID: "turn-old",
			}}
			c.applyResult("", tt.result)
			if c.state.Target != target || c.state.RuntimeEpoch != tt.epoch || c.state.SnapshotID != "" || c.state.CurrentTurnID != "" {
				t.Fatalf("client state after rotation = %+v", c.state)
			}
			value, err := c.authorizeParams(protocol.MethodSessionContext, protocol.SessionContextParams{})
			if err != nil {
				t.Fatal(err)
			}
			if got := value.(protocol.SessionContextParams).ExpectedRuntimeEpoch; got != tt.epoch {
				t.Fatalf("next request epoch = %q, want %q", got, tt.epoch)
			}
		})
	}
}

func TestLateSubscribeResultCannotUndoSessionMutation(t *testing.T) {
	target := protocol.RuntimeTarget{WorkspaceID: "workspace-live", SessionID: "session-live"}
	profile := protocol.ResolvedProfile{
		Model: "local/model", Effort: "high", CollaborationMode: protocol.CollaborationNormal,
		TokenMode: protocol.TokenFull, ToolApprovalMode: protocol.ToolApprovalAsk,
	}
	c := &Client{state: State{
		Initialized: true, HostEpoch: "host-live", WorkspaceID: target.WorkspaceID,
		Target: target, RuntimeEpoch: "runtime-old", SubscriptionID: "subscription-old", ResolvedProfile: profile,
	}}
	_, authority, err := c.authorizeRequest(protocol.MethodSessionSubscribe, protocol.SessionSubscribeParams{PageTurns: protocol.HistoryMaxTurns})
	if err != nil {
		t.Fatal(err)
	}
	c.applyResult(protocol.MethodSessionNew, protocol.SessionNewResult{
		SourceTarget: target, Target: target, RuntimeEpoch: "runtime-new", Disposition: "created", SnapshotRequired: true,
	})
	stale := protocol.SessionSubscribeResult{
		SubscriptionID: "subscription-stale",
		Snapshot: protocol.SessionSnapshot{
			SnapshotID: "snapshot-stale", HostEpoch: "host-live", Target: target,
			RuntimeEpoch: "runtime-old", Meta: protocol.SessionMetaSnapshot{ResolvedProfile: profile},
		},
	}
	if c.applyRequestResult(protocol.MethodSessionSubscribe, stale, authority) {
		t.Fatal("late subscribe result was accepted after the session authority changed")
	}
	if c.state.RuntimeEpoch != "runtime-new" || c.state.SubscriptionID != "subscription-old" {
		t.Fatalf("late subscribe changed client authority: %+v", c.state)
	}
}

func TestCurrentSnapshotRejectsStateChangedAfterSubscribe(t *testing.T) {
	target := protocol.RuntimeTarget{WorkspaceID: "workspace-live", SessionID: "session-live"}
	profile := protocol.ResolvedProfile{
		Model: "local/model", Effort: "high", CollaborationMode: protocol.CollaborationNormal,
		TokenMode: protocol.TokenFull, ToolApprovalMode: protocol.ToolApprovalAsk,
	}
	result := protocol.SessionSubscribeResult{
		SubscriptionID: "subscription-live",
		Snapshot: protocol.SessionSnapshot{
			SnapshotID: "snapshot-live", HostEpoch: "host-live", Target: target,
			RuntimeEpoch: "runtime-live", Meta: protocol.SessionMetaSnapshot{ResolvedProfile: profile},
		},
	}
	c := &Client{state: State{
		Initialized: true, HostEpoch: "host-live", WorkspaceID: target.WorkspaceID,
		Target: target, RuntimeEpoch: "runtime-live", ResolvedProfile: profile,
	}, completedTurns: map[protocol.TurnID]struct{}{}}
	c.applyResult(protocol.MethodSessionSubscribe, result)
	if !c.IsCurrentSnapshot(result) {
		t.Fatal("fresh subscribe result was not current")
	}
	c.applyResult(protocol.MethodSessionSubmit, protocol.SessionSubmitResult{
		Kind: protocol.SubmitTurn, TurnID: "turn-new", Target: target, RuntimeEpoch: "runtime-live",
	})
	if c.IsCurrentSnapshot(result) {
		t.Fatal("snapshot remained current after a newer turn started")
	}
}

func TestCurrentSnapshotRejectsAuthorityOnlyMutation(t *testing.T) {
	target := protocol.RuntimeTarget{WorkspaceID: "workspace-live", SessionID: "session-live"}
	profile := protocol.ResolvedProfile{
		Model: "local/model", Effort: "high", CollaborationMode: protocol.CollaborationNormal,
		TokenMode: protocol.TokenFull, ToolApprovalMode: protocol.ToolApprovalAsk,
	}
	result := protocol.SessionSubscribeResult{
		SubscriptionID: "subscription-live",
		Snapshot: protocol.SessionSnapshot{
			SnapshotID: "snapshot-live", HostEpoch: "host-live", Target: target,
			RuntimeEpoch: "runtime-live", Meta: protocol.SessionMetaSnapshot{ResolvedProfile: profile},
		},
	}
	c := &Client{state: State{
		Initialized: true, HostEpoch: "host-live", WorkspaceID: target.WorkspaceID,
		Target: target, RuntimeEpoch: "runtime-live", ResolvedProfile: profile,
	}, completedTurns: map[protocol.TurnID]struct{}{}}
	c.applyResult(protocol.MethodSessionSubscribe, result)
	if !c.IsCurrentSnapshot(result) {
		t.Fatal("fresh subscribe result was not current")
	}
	c.applyResult(protocol.MethodSessionGoalSet, protocol.SessionGoalSetResult{Goal: "ship", Status: protocol.GoalRunning})
	if c.IsCurrentSnapshot(result) {
		t.Fatal("snapshot remained current after an authority-only mutation")
	}
}

func TestSessionEventWaitsForSnapshotProjectionCommit(t *testing.T) {
	projected := make(chan struct{})
	delivered := make(chan struct{}, 1)
	c := &Client{
		notifyCh:       make(chan any, 1),
		completedTurns: map[protocol.TurnID]struct{}{},
		state:          State{SubscriptionID: "subscription-live"},
		callbacks: Callbacks{OnSessionEvent: func(protocol.SessionEvent) {
			select {
			case <-projected:
				delivered <- struct{}{}
			default:
				t.Error("session event ran before the snapshot projection committed")
			}
		}},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.projectionMu.Lock()
	go c.deliveryLoop(ctx)
	c.enqueue(protocol.SessionEvent{
		SubscriptionID: "subscription-live", Seq: 1,
		Event: eventwire.Event{Kind: "text", Text: "newer"},
	})
	select {
	case <-delivered:
		t.Fatal("session event passed the projection commit boundary")
	case <-time.After(50 * time.Millisecond):
	}
	close(projected)
	c.projectionMu.Unlock()
	select {
	case <-delivered:
	case <-time.After(time.Second):
		t.Fatal("session event was not delivered after the projection committed")
	}
}

func TestQueueOverflowSignalsResyncOutOfBand(t *testing.T) {
	resync := make(chan protocol.SessionResyncRequired, 1)
	c := &Client{
		notifyCh: make(chan any, 1),
		state: State{
			Initialized: true, SubscriptionID: "subscription_test", HostEpoch: "host_test",
			Target:       protocol.RuntimeTarget{WorkspaceID: "workspace_test", SessionID: "session_test"},
			RuntimeEpoch: "runtime_test",
		},
		callbacks: Callbacks{OnResyncRequired: func(value protocol.SessionResyncRequired) { resync <- value }},
	}
	c.notifyCh <- struct{}{}
	c.enqueue(protocol.SessionEvent{})
	select {
	case got := <-resync:
		if got.Reason != protocol.ResyncQueueOverflow || got.SubscriptionID != "subscription_test" {
			t.Fatalf("resync = %+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("queue overflow signal was lost behind the full queue")
	}
}

func TestUnexpectedCloseNotifiesOnceAfterInitialize(t *testing.T) {
	notified := make(chan struct{}, 2)
	ctx, cancel := context.WithCancel(context.Background())
	c := &Client{
		state: State{Initialized: true}, cancel: cancel,
		callbacks: Callbacks{OnDisconnected: func() { notified <- struct{}{} }},
	}
	c.close(false)
	c.close(false)
	select {
	case <-notified:
	case <-time.After(time.Second):
		t.Fatal("unexpected close did not notify")
	}
	select {
	case <-notified:
		t.Fatal("close notified more than once")
	default:
	}
	select {
	case <-ctx.Done():
	default:
		t.Fatal("close did not cancel client context")
	}
}

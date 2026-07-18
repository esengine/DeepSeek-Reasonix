package daemon

import (
	"context"
	"errors"

	"reasonix/internal/eventwire"
	"reasonix/internal/remote/host"
	"reasonix/internal/remote/idempotency"
	"reasonix/internal/remote/protocol"
)

// operationHandlerSet is kept separate from newTransport while other Phase 6
// handler families are landing in server.go. newTransport must merge these
// four exact entries into its HandlerSet; the methods below remain ordinary
// production transport handlers, not test-only routing shims.
func operationHandlerSet(t *transport) protocol.HandlerSet {
	return protocol.HandlerSet{
		protocol.MethodShellRun:         t.handleShellRun,
		protocol.MethodSessionCompact:   t.handleSessionCompact,
		protocol.MethodSessionSummarize: t.handleSessionSummarize,
		protocol.MethodOperationCancel:  t.handleOperationCancel,
	}
}

func (t *transport) handleShellRun(ctx context.Context, value any) (any, error) {
	params := value.(protocol.ShellRunParams)
	if err := t.validateLease(true); err != nil {
		return nil, err
	}
	request := idempotency.Request{
		RequestID: params.RequestID, Method: string(protocol.MethodShellRun),
		Target: idempotency.SessionTarget(params.Target), Params: params,
	}
	var replay protocol.OperationStartedResult
	if found, err := t.server.lookupMutation(ctx, request, &replay); found || err != nil {
		return replay, err
	}
	runtime, ok := t.server.runtimes.Runtime(params.Target)
	if !ok {
		err := t.server.rejectMissingSessionMutation(ctx, request, params.ExpectedHostEpoch, params.Target, &replay, t.sessionMutationGuard())
		return replay, err
	}
	return runtime.StartShellMutation(ctx, t.server.requests, params, t.sessionMutationGuard())
}

func (t *transport) handleSessionCompact(ctx context.Context, value any) (any, error) {
	params := value.(protocol.SessionCompactParams)
	if err := t.validateLease(true); err != nil {
		return nil, err
	}
	request := idempotency.Request{
		RequestID: params.RequestID, Method: string(protocol.MethodSessionCompact),
		Target: idempotency.SessionTarget(params.Target), Params: params,
	}
	var replay protocol.OperationStartedResult
	if found, err := t.server.lookupMutation(ctx, request, &replay); found || err != nil {
		return replay, err
	}
	runtime, ok := t.server.runtimes.Runtime(params.Target)
	if !ok {
		err := t.server.rejectMissingSessionMutation(ctx, request, params.ExpectedHostEpoch, params.Target, &replay, t.sessionMutationGuard())
		return replay, err
	}
	return runtime.StartCompactMutation(ctx, t.server.requests, params, t.sessionMutationGuard())
}

func (t *transport) handleSessionSummarize(ctx context.Context, value any) (any, error) {
	params := value.(protocol.SessionSummarizeParams)
	if err := t.validateLease(true); err != nil {
		return nil, err
	}
	request := idempotency.Request{
		RequestID: params.RequestID, Method: string(protocol.MethodSessionSummarize),
		Target: idempotency.SessionTarget(params.Target), Params: params,
	}
	var replay protocol.OperationStartedResult
	if found, err := t.server.lookupMutation(ctx, request, &replay); found || err != nil {
		return replay, err
	}
	runtime, ok := t.server.runtimes.Runtime(params.Target)
	if !ok {
		err := t.server.rejectMissingSessionMutation(ctx, request, params.ExpectedHostEpoch, params.Target, &replay, t.sessionMutationGuard())
		return replay, err
	}
	return runtime.StartSummarizeMutation(ctx, t.server.requests, params, t.sessionMutationGuard())
}

func (t *transport) handleOperationCancel(ctx context.Context, value any) (any, error) {
	params := value.(protocol.OperationCancelParams)
	if err := t.validateLease(true); err != nil {
		return nil, err
	}
	request := idempotency.Request{
		RequestID: params.RequestID, Method: string(protocol.MethodOperationCancel),
		Target: idempotency.SessionTarget(params.Target), Params: params,
	}
	var replay protocol.OperationCancelResult
	if found, err := t.server.lookupMutation(ctx, request, &replay); found || err != nil {
		return replay, err
	}
	runtime, ok := t.server.runtimes.Runtime(params.Target)
	if !ok {
		err := t.server.rejectMissingSessionMutation(ctx, request, params.ExpectedHostEpoch, params.Target, &replay, t.sessionMutationGuard())
		return replay, err
	}
	return runtime.CancelOperationMutation(ctx, t.server.requests, params, t.sessionMutationGuard())
}

// projectHostRuntimeState owns the target-neutral Host-to-protocol mapping for
// snapshot runtime state. projectSnapshot supplies metadata/history around it.
func projectHostRuntimeState(snapshot host.RuntimeSnapshot) (protocol.SessionRuntimeState, error) {
	if snapshot.CurrentTurn != "" && snapshot.CurrentOperation != nil {
		return protocol.SessionRuntimeState{}, errors.New("Host snapshot has both a Turn and an Operation")
	}
	if !snapshot.Running && (snapshot.CurrentTurn != "" || snapshot.CurrentOperation != nil) {
		return protocol.SessionRuntimeState{}, errors.New("idle Host snapshot retains foreground identity")
	}
	liveEvents := make([]eventwire.Event, len(snapshot.Events))
	for index := range snapshot.Events {
		liveEvents[index] = snapshot.Events[index].Event
	}
	state := protocol.SessionRuntimeState{
		Running: snapshot.Running, CancelRequested: snapshot.CancelRequested,
		LastOutcome: snapshot.LastOutcome, LiveEvents: liveEvents,
	}
	if snapshot.CurrentTurn != "" {
		state.CurrentTurn = &protocol.TurnState{
			TurnID: snapshot.CurrentTurn, CancelRequested: snapshot.CancelRequested,
		}
	}
	if snapshot.CurrentOperation != nil {
		operation := *snapshot.CurrentOperation
		if operation.OperationID == "" || operation.Kind == "" {
			return protocol.SessionRuntimeState{}, errors.New("Host snapshot Operation identity is incomplete")
		}
		if operation.CancelRequested != snapshot.CancelRequested {
			return protocol.SessionRuntimeState{}, errors.New("Host snapshot Operation cancel state is inconsistent")
		}
		state.CurrentOperation = &operation
	}
	if snapshot.PreviousTurnInterrupted {
		if snapshot.LastOutcome != protocol.OutcomeInterrupted || snapshot.InterruptionReason != protocol.InterruptionHostRestarted {
			return protocol.SessionRuntimeState{}, errors.New("invalid Host recovery interruption state")
		}
		state.Interruption = &protocol.RuntimeInterruption{
			PreviousTurnInterrupted: true,
			Reason:                  snapshot.InterruptionReason,
		}
	} else if snapshot.InterruptionReason != "" {
		return protocol.SessionRuntimeState{}, errors.New("Host interruption reason without an interrupted Turn")
	}
	if snapshot.LastError != "" {
		lastError := snapshot.LastError
		state.LastError = &lastError
	}
	return state, nil
}

// projectHostSessionEvent preserves the mutually-exclusive foreground opaque
// identity before contentRef externalization and protocol validation.
func projectHostSessionEvent(subscriptionID protocol.SubscriptionID, value host.RuntimeEvent) (protocol.SessionEvent, error) {
	if value.TurnID != "" && value.OperationID != "" {
		return protocol.SessionEvent{}, errors.New("Host event has both a Turn and an Operation")
	}
	return protocol.SessionEvent{
		SubscriptionID: subscriptionID,
		HostEpoch:      value.HostEpoch,
		Target:         value.Target,
		RuntimeEpoch:   value.RuntimeEpoch,
		Seq:            value.Seq,
		TurnID:         value.TurnID,
		OperationID:    value.OperationID,
		Event:          value.Event,
		Externalized:   []protocol.ExternalizedField{},
	}, nil
}

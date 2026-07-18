package host

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"reasonix/internal/control"
	"reasonix/internal/remote/idempotency"
	"reasonix/internal/remote/protocol"
)

var (
	ErrOperationNotActive         = errors.New("remote session runtime has no active operation")
	ErrOperationMismatch          = errors.New("remote operation does not match the active operation")
	ErrCheckpointNotFound         = errors.New("remote checkpoint is not available in this runtime")
	ErrCheckpointScopeUnavailable = errors.New("remote checkpoint does not support this operation")
	ErrInvalidOperationInput      = errors.New("remote operation input is invalid")
)

// OperationMismatchError is the non-wire strict-cancel diagnostic. Protocol
// mutations translate it to OPERATION_MISMATCH with the current and requested
// opaque identities; neither identity is ever used to find another handle.
type OperationMismatchError struct {
	Requested protocol.OperationID
	Current   protocol.OperationID
}

func (e *OperationMismatchError) Error() string {
	return fmt.Sprintf("%v: requested %q, current %q", ErrOperationMismatch, e.Requested, e.Current)
}

func (e *OperationMismatchError) Unwrap() error { return ErrOperationMismatch }

type operationMutation struct {
	method    protocol.Method
	requestID protocol.RequestID
	mutation  protocol.SessionMutation
	params    any
	kind      protocol.OperationKind
	buildSpec func(*runtimeActorState) (control.OperationSpec, error)
}

// StartShellMutation admits one non-PTY user Shell command. The Controller
// owns its worker context, so cancelling ctx after admission only abandons the
// response wait; it does not stop the accepted command.
func (r *SessionRuntime) StartShellMutation(
	ctx context.Context,
	registry *idempotency.Registry,
	params protocol.ShellRunParams,
	beforeBegin func() error,
) (protocol.OperationStartedResult, error) {
	command := strings.TrimSpace(params.Command)
	if command == "" {
		return protocol.OperationStartedResult{}, fmt.Errorf("%w: shell command is empty", ErrInvalidOperationInput)
	}
	return r.startOperationMutation(ctx, registry, operationMutation{
		method: protocol.MethodShellRun, requestID: params.RequestID,
		mutation: params.SessionMutation, params: params, kind: protocol.OperationShell,
		buildSpec: func(*runtimeActorState) (control.OperationSpec, error) {
			return control.OperationSpec{Kind: control.OperationShell, Command: command}, nil
		},
	}, beforeBegin)
}

// StartCompactMutation admits a transcript compaction without running the raw
// composer dispatcher. Instructions are optional and immutable after admission.
func (r *SessionRuntime) StartCompactMutation(
	ctx context.Context,
	registry *idempotency.Registry,
	params protocol.SessionCompactParams,
	beforeBegin func() error,
) (protocol.OperationStartedResult, error) {
	instructions := strings.TrimSpace(params.Instructions)
	return r.startOperationMutation(ctx, registry, operationMutation{
		method: protocol.MethodSessionCompact, requestID: params.RequestID,
		mutation: params.SessionMutation, params: params, kind: protocol.OperationCompact,
		buildSpec: func(*runtimeActorState) (control.OperationSpec, error) {
			return control.OperationSpec{Kind: control.OperationCompact, Instructions: instructions}, nil
		},
	}, beforeBegin)
}

// StartSummarizeMutation resolves the runtime-bound opaque checkpoint inside
// the actor and passes only its display turn to Controller. A removed or
// replacement-runtime checkpoint can never be reinterpreted as a later turn.
func (r *SessionRuntime) StartSummarizeMutation(
	ctx context.Context,
	registry *idempotency.Registry,
	params protocol.SessionSummarizeParams,
	beforeBegin func() error,
) (protocol.OperationStartedResult, error) {
	if strings.TrimSpace(string(params.CheckpointID)) == "" {
		return protocol.OperationStartedResult{}, fmt.Errorf("%w: checkpointId is empty", ErrInvalidOperationInput)
	}
	var direction control.SummarizeDirection
	switch params.Direction {
	case protocol.SummaryFrom:
		direction = control.SummarizeFrom
	case protocol.SummaryUpTo:
		direction = control.SummarizeUpTo
	default:
		return protocol.OperationStartedResult{}, fmt.Errorf("%w: unknown summary direction %q", ErrInvalidOperationInput, params.Direction)
	}
	return r.startOperationMutation(ctx, registry, operationMutation{
		method: protocol.MethodSessionSummarize, requestID: params.RequestID,
		mutation: params.SessionMutation, params: params, kind: protocol.OperationSummarize,
		buildSpec: func(state *runtimeActorState) (control.OperationSpec, error) {
			turn, err := r.resolveCheckpointTurnForState(state, params.CheckpointID, true)
			if err != nil {
				switch {
				case errors.Is(err, ErrCheckpointNotFound):
					return control.OperationSpec{}, runtimeRemoteError(protocol.ErrCheckpointNotFound, r.target, "", string(params.CheckpointID))
				case errors.Is(err, ErrCheckpointScopeUnavailable):
					return control.OperationSpec{}, runtimeRemoteError(protocol.ErrCheckpointScopeUnavailable, r.target, "", string(params.CheckpointID))
				default:
					return control.OperationSpec{}, err
				}
			}
			return control.OperationSpec{Kind: control.OperationSummarize, Turn: turn, Direction: direction}, nil
		},
	}, beforeBegin)
}

func (r *SessionRuntime) startOperationMutation(
	ctx context.Context,
	registry *idempotency.Registry,
	mutation operationMutation,
	beforeBegin func() error,
) (protocol.OperationStartedResult, error) {
	if registry == nil {
		return protocol.OperationStartedResult{}, errors.New("remote idempotency registry is required")
	}
	request := idempotency.Request{
		RequestID: mutation.requestID,
		Method:    string(mutation.method),
		Target:    idempotency.SessionTarget(mutation.mutation.Target),
		Params:    mutation.params,
	}
	value, err := r.call(ctx, func(state *runtimeActorState) (any, error) {
		if beforeBegin != nil {
			if err := beforeBegin(); err != nil {
				return nil, err
			}
		}
		if err := r.preadmitSessionMutation(mutation.mutation); err != nil {
			return nil, err
		}
		attempt, err := registry.Begin(request)
		if err != nil {
			return nil, err
		}
		claim, owns := attempt.Claim()
		if !owns {
			return mutationReplay{attempt: attempt}, nil
		}

		// running is the aggregate foreground gate. The specific fields keep
		// diagnostics and exact cancellation unambiguous even if a Controller
		// invariant is violated while an event is in flight.
		if state.currentTurn != "" || state.currentOperation != nil || state.pendingPrompt != nil || state.running {
			return nil, rejectMutation(claim, runtimeRemoteError(protocol.ErrSessionBusy, r.target, "", ""))
		}
		spec, err := mutation.buildSpec(state)
		if err != nil {
			var remote *protocol.RemoteError
			if errors.As(err, &remote) {
				return nil, rejectMutation(claim, err)
			}
			return nil, abortMutation(claim, err)
		}
		operationID, err := r.nextOperationID()
		if err != nil {
			return nil, abortMutation(claim, err)
		}
		result := protocol.OperationStartedResult{OperationID: operationID, Disposition: "started"}
		outcome, err := idempotency.PrepareSuccess(result)
		if err != nil {
			return nil, abortMutation(claim, err)
		}

		handle, err := safeStartOperation(r.controller, spec)
		if err != nil {
			switch {
			case errors.Is(err, control.ErrSessionBusy):
				return nil, rejectMutation(claim, runtimeRemoteError(protocol.ErrSessionBusy, r.target, "", ""))
			case errors.Is(err, control.ErrOperationUnavailable):
				return nil, rejectMutation(claim, runtimeRemoteError(protocol.ErrCapabilityUnavailable, r.target, "", string(mutation.kind)))
			default:
				return nil, abortMutation(claim, err)
			}
		}
		if handle == nil {
			return nil, abortMutation(claim, errors.New("Controller admitted an Operation without a handle"))
		}

		state.live.Reset()
		state.liveOperationID = operationID
		state.running = true
		state.currentTurn = ""
		state.acceptedTurn = nil
		state.cancelRequested = false
		state.lastError = ""
		state.currentOperation = &currentOperationState{
			id: operationID, kind: mutation.kind, handle: handle,
		}
		go r.awaitOperation(operationID, handle)
		if err := claim.Resolve(outcome); err != nil {
			// Controller admission and actor state are already committed. Never
			// cancel or repeat real work merely because the short response cache
			// could not publish an outcome during daemon shutdown.
			return nil, err
		}
		return result, nil
	})
	if err != nil {
		return protocol.OperationStartedResult{}, err
	}
	if replay, ok := value.(mutationReplay); ok {
		outcome, err := replay.attempt.Wait(ctx)
		if err != nil {
			return protocol.OperationStartedResult{}, err
		}
		var result protocol.OperationStartedResult
		if err := outcome.Decode(&result); err != nil {
			return protocol.OperationStartedResult{}, err
		}
		return result, nil
	}
	result, ok := value.(protocol.OperationStartedResult)
	if !ok {
		return protocol.OperationStartedResult{}, errors.New("remote Operation actor returned an invalid result")
	}
	return result, nil
}

func (r *SessionRuntime) nextOperationID() (protocol.OperationID, error) {
	if r.opts.NewOperationID == nil {
		return "", errors.New("remote Operation ID generator is unavailable")
	}
	id, err := r.opts.NewOperationID()
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(string(id)) == "" {
		return "", errors.New("generated Operation ID is empty")
	}
	return id, nil
}

func safeStartOperation(controller control.SessionAPI, spec control.OperationSpec) (handle *control.OperationHandle, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			handle = nil
			err = fmt.Errorf("Controller StartOperation panicked: %v", recovered)
		}
	}()
	return controller.StartOperation(spec)
}

// awaitOperation is rooted in the daemon-owned runtime, not the attach request.
// The Controller resolves Done after releasing its foreground gate. Completion
// is then serialized with later RPCs and clears only the exact opaque ID+handle
// pair that was admitted by this runtime incarnation.
func (r *SessionRuntime) awaitOperation(operationID protocol.OperationID, handle *control.OperationHandle) {
	result, ok := <-handle.Done()
	if !ok {
		result.Err = errors.New("Controller Operation ended without a result")
	}
	r.mailbox.enqueue(func(state *runtimeActorState) {
		current := state.currentOperation
		if current == nil || current.id != operationID || current.handle != handle {
			return
		}
		state.currentOperation = nil
		state.running = false
		state.cancelRequested = false
		state.acceptedTurn = nil
		switch {
		case result.Err == nil:
			state.lastOutcome = protocol.OutcomeCompleted
			state.lastError = ""
		case errors.Is(result.Err, context.Canceled):
			state.lastOutcome = protocol.OutcomeCancelled
			state.lastError = ""
		default:
			state.lastOutcome = protocol.OutcomeFailed
			state.lastError = result.Err.Error()
		}
	})
}

// CancelOperation strictly targets the currently admitted handle. It is the
// actor-level primitive used by the requestId-aware protocol mutation below.
func (r *SessionRuntime) CancelOperation(ctx context.Context, expected protocol.OperationID) (protocol.OperationCancelResult, error) {
	value, err := r.call(ctx, func(state *runtimeActorState) (any, error) {
		return cancelOperationForState(state, expected)
	})
	if err != nil {
		return protocol.OperationCancelResult{}, err
	}
	return value.(protocol.OperationCancelResult), nil
}

func cancelOperationForState(state *runtimeActorState, expected protocol.OperationID) (protocol.OperationCancelResult, error) {
	current := state.currentOperation
	if current == nil {
		return protocol.OperationCancelResult{}, ErrOperationNotActive
	}
	if expected == "" || expected != current.id {
		return protocol.OperationCancelResult{}, &OperationMismatchError{Requested: expected, Current: current.id}
	}
	attempt := current.handle.Cancel()
	var status protocol.CancelStatus
	switch attempt {
	case control.OperationCancelRequestedNow:
		status = protocol.CancelRequested
	case control.OperationCancelAlreadyRequested:
		status = protocol.CancelAlreadyRequested
	default:
		return protocol.OperationCancelResult{}, ErrOperationNotActive
	}
	current.cancelRequested = true
	state.cancelRequested = true
	return protocol.OperationCancelResult{Status: status, OperationID: current.id}, nil
}

// CancelOperationMutation preserves the first strict cancel outcome even when
// the Operation completes before a response-loss retry arrives.
func (r *SessionRuntime) CancelOperationMutation(
	ctx context.Context,
	registry *idempotency.Registry,
	params protocol.OperationCancelParams,
	beforeBegin func() error,
) (protocol.OperationCancelResult, error) {
	if registry == nil {
		return protocol.OperationCancelResult{}, errors.New("remote idempotency registry is required")
	}
	request := idempotency.Request{
		RequestID: params.RequestID,
		Method:    string(protocol.MethodOperationCancel),
		Target:    idempotency.SessionTarget(params.Target),
		Params:    params,
	}
	value, err := r.call(ctx, func(state *runtimeActorState) (any, error) {
		if beforeBegin != nil {
			if err := beforeBegin(); err != nil {
				return nil, err
			}
		}
		if err := r.preadmitSessionMutation(params.SessionMutation); err != nil {
			return nil, err
		}
		attempt, err := registry.Begin(request)
		if err != nil {
			return nil, err
		}
		claim, owns := attempt.Claim()
		if !owns {
			return mutationReplay{attempt: attempt}, nil
		}
		result, err := cancelOperationForState(state, params.ExpectedOperationID)
		if err != nil {
			switch {
			case errors.Is(err, ErrOperationNotActive):
				return nil, rejectMutation(claim, runtimeRemoteError(protocol.ErrOperationNotActive, r.target, "", ""))
			case errors.Is(err, ErrOperationMismatch):
				current := protocol.OperationID("")
				if state.currentOperation != nil {
					current = state.currentOperation.id
				}
				return nil, rejectMutation(claim, runtimeRemoteError(
					protocol.ErrOperationMismatch, r.target, string(current), string(params.ExpectedOperationID),
				))
			default:
				return nil, abortMutation(claim, err)
			}
		}
		outcome, err := idempotency.PrepareSuccess(result)
		if err != nil {
			return nil, abortMutation(claim, err)
		}
		if err := claim.Resolve(outcome); err != nil {
			return nil, err
		}
		return result, nil
	})
	if err != nil {
		return protocol.OperationCancelResult{}, err
	}
	if replay, ok := value.(mutationReplay); ok {
		outcome, err := replay.attempt.Wait(ctx)
		if err != nil {
			return protocol.OperationCancelResult{}, err
		}
		var result protocol.OperationCancelResult
		if err := outcome.Decode(&result); err != nil {
			return protocol.OperationCancelResult{}, err
		}
		return result, nil
	}
	result, ok := value.(protocol.OperationCancelResult)
	if !ok {
		return protocol.OperationCancelResult{}, errors.New("remote Operation cancel actor returned an invalid result")
	}
	return result, nil
}

// ResolveCheckpointTurn is the actor-safe identity resolver shared by
// summarize now and fork/rewind later. It reconciles current Controller
// checkpoints first, so an ID removed by a rewrite becomes immediately stale.
func (r *SessionRuntime) ResolveCheckpointTurn(ctx context.Context, checkpointID protocol.CheckpointID) (int, error) {
	value, err := r.call(ctx, func(state *runtimeActorState) (any, error) {
		return r.resolveCheckpointTurnForState(state, checkpointID, false)
	})
	if err != nil {
		return 0, err
	}
	return value.(int), nil
}

func (r *SessionRuntime) resolveCheckpointTurnForState(
	state *runtimeActorState,
	checkpointID protocol.CheckpointID,
	requireConversation bool,
) (int, error) {
	if strings.TrimSpace(string(checkpointID)) == "" {
		return 0, ErrCheckpointNotFound
	}
	var snapshot control.CheckpointSnapshot
	if err := safeControllerCall(func() { snapshot = r.controller.CheckpointSnapshot() }); err != nil {
		return 0, err
	}
	reconciled, err := r.reconcileCheckpointIDs(state, snapshot.Metas)
	if err != nil {
		return 0, err
	}
	state.checkpointIDs = reconciled
	for turn, currentID := range reconciled {
		if currentID != checkpointID {
			continue
		}
		if requireConversation && !snapshot.ConversationAvailable[turn] {
			return 0, ErrCheckpointScopeUnavailable
		}
		return turn, nil
	}
	return 0, ErrCheckpointNotFound
}

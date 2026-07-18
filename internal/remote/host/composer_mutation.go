package host

import (
	"context"
	"errors"
	"fmt"

	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/eventwire"
	"reasonix/internal/remote/idempotency"
	"reasonix/internal/remote/protocol"
	"reasonix/internal/runtimeapi"
	"reasonix/internal/runtimeservice"
	"reasonix/internal/skill"
)

// ComposerDelegationError is returned before requestId registration when a raw
// composer command requires RuntimeManager/catalog coordination. The daemon
// delegates these routes to SessionLifecycleService while retaining the
// original session/submit request identity and result union.
type ComposerDelegationError struct {
	Route runtimeservice.ComposerRoute
}

func (e *ComposerDelegationError) Error() string {
	return fmt.Sprintf("remote composer route %q requires lifecycle coordination", e.Route.Lifecycle)
}

// ComposerSubmitMutation is the real non-lifecycle session/submit dispatcher.
// Classification, requestId admission, Controller admission, and actor state
// commit share one sequencer action. Accepted work is rooted in the runtime,
// not in ctx, and therefore survives an attach response loss or disconnect.
func (r *SessionRuntime) ComposerSubmitMutation(
	ctx context.Context,
	registry *idempotency.Registry,
	params protocol.SessionSubmitParams,
	beforeBegin func() error,
) (protocol.SessionSubmitResult, error) {
	if registry == nil {
		return protocol.SessionSubmitResult{}, errors.New("remote idempotency registry is required")
	}
	request := idempotency.Request{
		RequestID: params.RequestID,
		Method:    string(protocol.MethodSessionSubmit),
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
		route, err := r.composerRoute(params)
		if err != nil {
			return nil, err
		}
		if route.Kind == runtimeservice.ComposerLifecycle || route.Completion == runtimeservice.ComposerCompletionProfileEffort {
			return nil, &ComposerDelegationError{Route: route}
		}

		attempt, err := registry.Begin(request)
		if err != nil {
			return nil, err
		}
		claim, owns := attempt.Claim()
		if !owns {
			return mutationReplay{attempt: attempt}, nil
		}
		if state.currentTurn != "" || state.currentOperation != nil || state.pendingPrompt != nil || state.running {
			return nil, rejectMutation(claim, runtimeRemoteError(protocol.ErrSessionBusy, r.target, "", ""))
		}

		switch route.Kind {
		case runtimeservice.ComposerTurn:
			return r.admitComposerTurn(state, claim, params, route)
		case runtimeservice.ComposerOperation:
			return r.admitComposerOperation(state, claim, params, route)
		case runtimeservice.ComposerCompleted:
			return r.admitComposerCompletion(state, claim, route)
		default:
			return nil, abortMutation(claim, errors.New("remote composer dispatcher returned an unsupported route"))
		}
	})
	if err != nil {
		return protocol.SessionSubmitResult{}, err
	}
	if replay, ok := value.(mutationReplay); ok {
		outcome, err := replay.attempt.Wait(ctx)
		if err != nil {
			return protocol.SessionSubmitResult{}, err
		}
		var result protocol.SessionSubmitResult
		if err := outcome.Decode(&result); err != nil {
			return protocol.SessionSubmitResult{}, err
		}
		return result, nil
	}
	result, ok := value.(protocol.SessionSubmitResult)
	if !ok {
		return protocol.SessionSubmitResult{}, errors.New("remote composer actor returned an invalid result")
	}
	return result, nil
}

func (r *SessionRuntime) composerRoute(params protocol.SessionSubmitParams) (runtimeservice.ComposerRoute, error) {
	symbols, err := r.composerSymbols()
	if err != nil {
		return runtimeservice.ComposerRoute{}, err
	}
	invocations := make([]runtimeapi.Invocation, len(params.Invocations))
	for index, invocation := range params.Invocations {
		invocations[index] = runtimeapi.Invocation{
			Name: invocation.Name,
			Kind: runtimeapi.InvocationKind(invocation.Kind),
		}
	}
	return runtimeservice.RouteComposerSubmit(runtimeapi.ComposerSubmitInput{
		Session: runtimeapi.SessionRef{
			WorkspaceID: runtimeapi.WorkspaceID(params.Target.WorkspaceID),
			SessionID:   runtimeapi.SessionID(params.Target.SessionID),
		},
		Input: params.Input, DisplayText: params.DisplayText, EditedOriginal: params.EditedOriginal,
		Invocations: invocations, DeliveryRecovery: params.DeliveryRecovery,
	}, symbols)
}

func (r *SessionRuntime) composerSymbols() (symbols runtimeservice.ComposerSymbols, err error) {
	err = safeControllerCall(func() {
		for _, command := range r.controller.Commands() {
			if !command.Hidden {
				symbols.TurnCommands = append(symbols.TurnCommands, command.Name)
			}
		}
		if host := r.controller.Host(); host != nil {
			for _, prompt := range host.Prompts() {
				symbols.TurnCommands = append(symbols.TurnCommands, prompt.Name)
			}
		}
		for _, invocation := range r.controller.SlashSkills() {
			kind := runtimeapi.InvocationSkill
			if invocation.RunAs == skill.RunSubagent {
				kind = runtimeapi.InvocationSubagent
			}
			symbols.Invocations = append(symbols.Invocations, runtimeservice.ComposerInvocationSymbol{
				Name: invocation.SlashName(), Kind: kind,
			})
		}
	})
	return symbols, err
}

func (r *SessionRuntime) admitComposerTurn(
	state *runtimeActorState,
	claim *idempotency.Claim,
	params protocol.SessionSubmitParams,
	route runtimeservice.ComposerRoute,
) (any, error) {
	turnID, err := r.nextTurnID(state)
	if err != nil {
		return nil, abortMutation(claim, err)
	}
	result := protocol.SessionSubmitResult{
		Kind: protocol.SubmitTurn, TurnID: turnID,
		Target: r.target, RuntimeEpoch: r.epoch,
	}
	outcome, err := idempotency.PrepareSuccess(result)
	if err != nil {
		return nil, abortMutation(claim, err)
	}
	accepted, err := r.acceptedTurn(turnID, params.Input, route.DisplayText)
	if err != nil {
		return nil, abortMutation(claim, err)
	}
	completeDurableAdmission := func() control.DurableTurnAdmissionResult {
		// Compatibility controllers without the optional boundary retain their
		// historical immediate admission semantics.
		return control.DurableTurnAdmissionResult{Claimed: true, SemanticCommit: true}
	}
	if durable, ok := r.controller.(control.DurableTurnAdmission); ok {
		// Prepare is the semantic linearization point: the Controller writes the
		// top-level in-flight marker, appends the stable user message (including
		// edit metadata), and commits the authoritative transcript before any
		// guarded body, hook, planner, provider, or tool can run. Keep the raw wire
		// composer fields here; route.Input may already contain command expansion.
		completeDurableAdmission, err = durable.PrepareDurableTurn(control.DurableTurnInput{
			Input: params.Input, DisplayText: params.DisplayText, EditedOriginal: params.EditedOriginal,
		})
		if err != nil {
			target := r.target
			failure := protocol.MustRemoteError(protocol.ErrSessionPersistFailed, protocol.ErrorOptions{Target: &target})
			return nil, abortMutation(claim, failure)
		}
	}

	state.running = true
	state.currentTurn = turnID
	state.acceptedTurn = accepted
	submitErr := safeControllerCall(func() {
		switch route.Turn {
		case runtimeservice.ComposerTurnNormal:
			r.controller.SubmitDisplay(route.DisplayText, route.Input)
		case runtimeservice.ComposerTurnEdited:
			r.controller.SubmitEditedDisplay(route.DisplayText, route.Input, route.EditedOriginal)
		case runtimeservice.ComposerTurnInvocations:
			requests := make([]control.InvocationRequest, len(route.Invocations))
			for index, invocation := range route.Invocations {
				requests[index] = control.InvocationRequest{Name: invocation.Name, Kind: string(invocation.Kind), Offset: index}
			}
			r.controller.SubmitInvocationDisplay(route.DisplayText, route.Input, requests)
		case runtimeservice.ComposerTurnRecovery:
			r.controller.SubmitDeliveryRecovery(route.DisplayText, route.Input)
		case runtimeservice.ComposerTurnRawSlash:
			if route.EditedOriginal != "" {
				r.controller.SubmitEditedDisplay(route.DisplayText, route.Input, route.EditedOriginal)
			} else {
				r.controller.SubmitDisplay(route.DisplayText, route.Input)
			}
		default:
			panic("unsupported composer Turn primitive")
		}
	})
	admission := completeDurableAdmission()
	// A successful Prepare call already committed the transcript; completion only
	// reports whether Submit claimed the reserved guarded body. Returning an RPC
	// rejection after that point would
	// create a recorded-but-unaddressable ghost user turn. A post-commit metadata
	// diagnostic is therefore non-fatal. Prepare also reserves the exact guarded
	// body, so a production Controller must claim it; keep the success result
	// authoritative even if an injected compatibility controller reports an
	// auxiliary diagnostic in admission.Err.
	if submitErr != nil || !admission.Claimed || !admission.SemanticCommit {
		// This is an invariant-recovery path, not an externally retryable
		// admission failure. Preserve the successful opaque Turn result while
		// terminating Host live state deterministically if Controller dispatch
		// itself panicked or failed to claim its reserved body.
		failureText := "Controller did not claim the prepared durable Turn"
		if admission.Err != nil {
			failureText = admission.Err.Error()
		}
		if submitErr != nil {
			failureText = submitErr.Error()
		}
		r.applyEvent(state, event.TurnDone, eventwire.ToWire(event.Event{Kind: event.TurnDone, Err: errors.New(failureText)}))
	}
	state.cancelRequested = false
	if state.running {
		state.lastError = ""
		state.live.Reset()
		state.liveOperationID = ""
	}
	if err := claim.Resolve(outcome); err != nil {
		return nil, err
	}
	return result, nil
}

func (r *SessionRuntime) admitComposerOperation(
	state *runtimeActorState,
	claim *idempotency.Claim,
	_ protocol.SessionSubmitParams,
	route runtimeservice.ComposerRoute,
) (any, error) {
	var spec control.OperationSpec
	switch route.Operation {
	case runtimeapi.OperationShell:
		spec = control.OperationSpec{Kind: control.OperationShell, Command: route.Argument}
	case runtimeapi.OperationCompact:
		spec = control.OperationSpec{Kind: control.OperationCompact, Instructions: route.Argument}
	default:
		return nil, abortMutation(claim, errors.New("unsupported composer Operation"))
	}
	operationID, err := r.nextOperationID()
	if err != nil {
		return nil, abortMutation(claim, err)
	}
	operationKind := protocol.OperationKind(route.Operation)
	result := protocol.SessionSubmitResult{
		Kind: protocol.SubmitOperation, OperationID: operationID, Operation: operationKind,
		Target: r.target, RuntimeEpoch: r.epoch,
	}
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
			return nil, rejectMutation(claim, runtimeRemoteError(protocol.ErrCapabilityUnavailable, r.target, "", string(operationKind)))
		default:
			return nil, abortMutation(claim, err)
		}
	}
	if handle == nil {
		return nil, abortMutation(claim, errors.New("Controller admitted a composer Operation without a handle"))
	}
	state.live.Reset()
	state.liveOperationID = operationID
	state.running = true
	state.currentTurn = ""
	state.acceptedTurn = nil
	state.cancelRequested = false
	state.lastError = ""
	state.currentOperation = &currentOperationState{id: operationID, kind: operationKind, handle: handle}
	go r.awaitOperation(operationID, handle)
	if err := claim.Resolve(outcome); err != nil {
		return nil, err
	}
	return result, nil
}

func (r *SessionRuntime) admitComposerCompletion(
	state *runtimeActorState,
	claim *idempotency.Claim,
	route runtimeservice.ComposerRoute,
) (any, error) {
	result := protocol.SessionSubmitResult{
		Kind: protocol.SubmitCompleted, Effect: protocol.SubmitEffect(route.Effect),
		Target: r.target, RuntimeEpoch: r.epoch,
	}
	outcome, err := idempotency.PrepareSuccess(result)
	if err != nil {
		return nil, abortMutation(claim, err)
	}
	switch route.Completion {
	case runtimeservice.ComposerCompletionMemoryRemember:
		if state.memoryResearchErr != nil || state.memoryResearch == nil {
			return nil, rejectMutation(claim, runtimeRemoteError(protocol.ErrCapabilityUnavailable, r.target, "", "memory"))
		}
		_, err = state.memoryResearch.Remember(runtimeapi.SessionRef{
			WorkspaceID: runtimeapi.WorkspaceID(r.target.WorkspaceID), SessionID: runtimeapi.SessionID(r.target.SessionID),
		}, r.controller, "project", route.Argument)
		if err == nil {
			r.emit(event.Event{Kind: event.Notice, Text: "remembered"})
		}
	case runtimeservice.ComposerCompletionGoalStatus,
		runtimeservice.ComposerCompletionGoalClear,
		runtimeservice.ComposerCompletionNotice:
		err = safeControllerCall(func() { r.controller.SubmitDisplay(route.DisplayText, route.Input) })
	case runtimeservice.ComposerCompletionHostWriteDenied:
		r.emit(event.Event{
			Kind: event.Notice,
			Text: "This Host management command is read-only in Remote V1; run the corresponding reasonix CLI/config action on the Host.",
		})
	default:
		err = errors.New("unsupported composer completion")
	}
	if err != nil {
		if errors.Is(err, runtimeservice.ErrInvalidMemoryInput) || errors.Is(err, runtimeservice.ErrCapabilityUnavailable) {
			return nil, rejectMutation(claim, runtimeRemoteError(protocol.ErrCapabilityUnavailable, r.target, "", "memory"))
		}
		return nil, abortMutation(claim, err)
	}
	if err := claim.Resolve(outcome); err != nil {
		return nil, err
	}
	return result, nil
}

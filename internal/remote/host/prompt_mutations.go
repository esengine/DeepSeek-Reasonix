package host

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"reasonix/internal/event"
	"reasonix/internal/remote/idempotency"
	"reasonix/internal/remote/protocol"
)

var (
	ErrInvalidSteer          = errors.New("remote steer text is empty")
	ErrInvalidPromptDecision = errors.New("remote Prompt decision is invalid")
	ErrInvalidPromptAnswer   = errors.New("remote Prompt answer is invalid")
)

// SteerMutation implements strict opaque-Turn steering. It never falls back to
// Controller.Steer, so a stale request cannot start or park a later Turn.
func (r *SessionRuntime) SteerMutation(
	ctx context.Context,
	registry *idempotency.Registry,
	params protocol.TurnSteerParams,
	beforeBegin func() error,
) (protocol.TurnSteerResult, error) {
	if registry == nil {
		return protocol.TurnSteerResult{}, errors.New("remote idempotency registry is required")
	}
	if strings.TrimSpace(params.Text) == "" {
		return protocol.TurnSteerResult{}, ErrInvalidSteer
	}
	request := idempotency.Request{
		RequestID: params.RequestID,
		Method:    string(protocol.MethodTurnSteer),
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
		if state.currentTurn == "" {
			return nil, rejectMutation(claim, runtimeRemoteError(protocol.ErrTurnNotActive, r.target, "", ""))
		}
		if params.ExpectedTurnID == "" || params.ExpectedTurnID != state.currentTurn {
			return nil, rejectMutation(claim, runtimeRemoteError(
				protocol.ErrTurnMismatch, r.target, string(state.currentTurn), string(params.ExpectedTurnID),
			))
		}
		result := protocol.TurnSteerResult{Accepted: true, TurnID: state.currentTurn}
		outcome, err := idempotency.PrepareSuccess(result)
		if err != nil {
			return nil, abortMutation(claim, err)
		}
		accepted := false
		if err := safeControllerCall(func() { accepted = r.controller.TrySteer(params.Text) }); err != nil {
			return nil, abortMutation(claim, err)
		}
		if !accepted {
			return nil, rejectMutation(claim, runtimeRemoteError(protocol.ErrTurnNotActive, r.target, "", ""))
		}
		if err := claim.Resolve(outcome); err != nil {
			return nil, err
		}
		return result, nil
	})
	if err != nil {
		return protocol.TurnSteerResult{}, err
	}
	if replay, ok := value.(mutationReplay); ok {
		return replaySteer(ctx, replay.attempt)
	}
	result, ok := value.(protocol.TurnSteerResult)
	if !ok {
		return protocol.TurnSteerResult{}, errors.New("remote steer actor returned an invalid result")
	}
	return result, nil
}

// ApproveMutation resolves exactly the currently pending opaque Approval. The
// Host ID is retired before the Controller-private ID is invoked, within the
// same actor action and idempotency claim.
func (r *SessionRuntime) ApproveMutation(
	ctx context.Context,
	registry *idempotency.Registry,
	params protocol.PromptApproveParams,
	beforeBegin func() error,
) (protocol.PromptResolvedResult, error) {
	if registry == nil {
		return protocol.PromptResolvedResult{}, errors.New("remote idempotency registry is required")
	}
	if !validPromptDecision(params.Decision) {
		return protocol.PromptResolvedResult{}, fmt.Errorf("%w: unknown decision %q", ErrInvalidPromptDecision, params.Decision)
	}
	request := idempotency.Request{
		RequestID: params.RequestID,
		Method:    string(protocol.MethodPromptApprove),
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
		pending := state.pendingPrompt
		if pending == nil || pending.id != params.PromptID {
			return nil, rejectMutation(claim, runtimeRemoteError(protocol.ErrPromptNotPending, r.target, "", ""))
		}
		if pending.kind != protocol.PromptApproval {
			return nil, rejectMutation(claim, runtimeRemoteError(
				protocol.ErrPromptKindMismatch, r.target, string(protocol.PromptApproval), string(pending.kind),
			))
		}
		if !promptDecisionAllowed(pending.public.Approval, params.Decision) {
			return nil, rejectMutation(claim, runtimeRemoteError(
				protocol.ErrPromptDecisionNotAllowed, r.target,
				formatPromptDecisions(pending.public.Approval.AllowedDecisions), string(params.Decision),
			))
		}
		result := protocol.PromptResolvedResult{Resolved: true, PromptID: pending.id}
		outcome, err := idempotency.PrepareSuccess(result)
		if err != nil {
			return nil, abortMutation(claim, err)
		}
		allow, session, persist := approvalDecisionFlags(params.Decision)
		controllerID := pending.controllerID
		clearPendingPrompt(state)
		if err := safeControllerCall(func() { r.controller.Approve(controllerID, allow, session, persist) }); err != nil {
			// The opaque ID remains retired: after an indeterminate Controller
			// panic, retrying the side effect would be less safe than requiring a
			// fresh Prompt/Turn recovery path.
			return nil, abortMutation(claim, err)
		}
		if err := claim.Resolve(outcome); err != nil {
			return nil, err
		}
		return result, nil
	})
	if err != nil {
		return protocol.PromptResolvedResult{}, err
	}
	if replay, ok := value.(mutationReplay); ok {
		return replayPromptResolved(ctx, replay.attempt)
	}
	result, ok := value.(protocol.PromptResolvedResult)
	if !ok {
		return protocol.PromptResolvedResult{}, errors.New("remote approval actor returned an invalid result")
	}
	return result, nil
}

// AnswerMutation validates the response against the exact pending Ask before
// converting opaque wire DTOs back to Controller-private correlation values.
func (r *SessionRuntime) AnswerMutation(
	ctx context.Context,
	registry *idempotency.Registry,
	params protocol.PromptAnswerParams,
	beforeBegin func() error,
) (protocol.PromptResolvedResult, error) {
	if registry == nil {
		return protocol.PromptResolvedResult{}, errors.New("remote idempotency registry is required")
	}
	if err := params.Validate(); err != nil {
		return protocol.PromptResolvedResult{}, fmt.Errorf("%w: %v", ErrInvalidPromptAnswer, err)
	}
	request := idempotency.Request{
		RequestID: params.RequestID,
		Method:    string(protocol.MethodPromptAnswer),
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
		pending := state.pendingPrompt
		if pending == nil || pending.id != params.PromptID {
			return nil, rejectMutation(claim, runtimeRemoteError(protocol.ErrPromptNotPending, r.target, "", ""))
		}
		if pending.kind != protocol.PromptAsk {
			return nil, rejectMutation(claim, runtimeRemoteError(
				protocol.ErrPromptKindMismatch, r.target, string(protocol.PromptAsk), string(pending.kind),
			))
		}
		answers, err := validateAskAnswers(pending.public.Ask, params.Answers)
		if err != nil {
			// The wire shape has already passed protocol validation. A rejection
			// here depends on the exact pending Ask (unknown question, invalid
			// selection cardinality, or a mixed custom answer), so it is a
			// deterministic post-admission business decision. Cache the frozen
			// Prompt rejection instead of aborting the claim and allowing the same
			// requestId to observe different Prompt state later.
			return nil, rejectMutation(claim, runtimeRemoteError(
				protocol.ErrPromptDecisionNotAllowed, r.target, "", "",
			))
		}
		result := protocol.PromptResolvedResult{Resolved: true, PromptID: pending.id}
		outcome, err := idempotency.PrepareSuccess(result)
		if err != nil {
			return nil, abortMutation(claim, err)
		}
		controllerID := pending.controllerID
		clearPendingPrompt(state)
		if err := safeControllerCall(func() { r.controller.AnswerQuestion(controllerID, answers) }); err != nil {
			return nil, abortMutation(claim, err)
		}
		if err := claim.Resolve(outcome); err != nil {
			return nil, err
		}
		return result, nil
	})
	if err != nil {
		return protocol.PromptResolvedResult{}, err
	}
	if replay, ok := value.(mutationReplay); ok {
		return replayPromptResolved(ctx, replay.attempt)
	}
	result, ok := value.(protocol.PromptResolvedResult)
	if !ok {
		return protocol.PromptResolvedResult{}, errors.New("remote Ask actor returned an invalid result")
	}
	return result, nil
}

func replaySteer(ctx context.Context, attempt idempotency.Attempt) (protocol.TurnSteerResult, error) {
	outcome, err := attempt.Wait(ctx)
	if err != nil {
		return protocol.TurnSteerResult{}, err
	}
	var result protocol.TurnSteerResult
	if err := outcome.Decode(&result); err != nil {
		return protocol.TurnSteerResult{}, err
	}
	return result, nil
}

func replayPromptResolved(ctx context.Context, attempt idempotency.Attempt) (protocol.PromptResolvedResult, error) {
	outcome, err := attempt.Wait(ctx)
	if err != nil {
		return protocol.PromptResolvedResult{}, err
	}
	var result protocol.PromptResolvedResult
	if err := outcome.Decode(&result); err != nil {
		return protocol.PromptResolvedResult{}, err
	}
	return result, nil
}

func runtimeRemoteError(code protocol.ReasonixErrorCode, target protocol.RuntimeTarget, expected, actual string) error {
	remoteErr, err := protocol.NewRemoteError(code, protocol.ErrorOptions{
		Target: &target, Expected: expected, Actual: actual,
	})
	if err == nil {
		return remoteErr
	}
	// expected/actual can include a caller-supplied opaque ID. The protocol
	// deliberately rejects path-like diagnostic tokens; omit unsafe diagnostics
	// instead of letting malformed input turn a domain rejection into a Host
	// panic. The error code and target still carry the recovery contract.
	remoteErr, fallbackErr := protocol.NewRemoteError(code, protocol.ErrorOptions{Target: &target})
	if fallbackErr != nil {
		return fmt.Errorf("build Remote error %s: %w", code, fallbackErr)
	}
	return remoteErr
}

func validPromptDecision(decision protocol.PromptDecision) bool {
	switch decision {
	case protocol.DecisionAllowOnce, protocol.DecisionAllowSession, protocol.DecisionAllowPersistent, protocol.DecisionDeny:
		return true
	default:
		return false
	}
}

func promptDecisionAllowed(prompt *protocol.ApprovalPrompt, decision protocol.PromptDecision) bool {
	if prompt == nil {
		return false
	}
	for _, allowed := range prompt.AllowedDecisions {
		if allowed == decision {
			return true
		}
	}
	return false
}

func approvalDecisionFlags(decision protocol.PromptDecision) (allow, session, persist bool) {
	switch decision {
	case protocol.DecisionAllowOnce:
		return true, false, false
	case protocol.DecisionAllowSession:
		return true, true, false
	case protocol.DecisionAllowPersistent:
		return true, true, true
	default:
		return false, false, false
	}
}

func formatPromptDecisions(decisions []protocol.PromptDecision) string {
	values := make([]string, len(decisions))
	for index, decision := range decisions {
		values[index] = string(decision)
	}
	return strings.Join(values, ",")
}

func validateAskAnswers(prompt *protocol.AskPrompt, answers []protocol.QuestionAnswer) ([]event.AskAnswer, error) {
	if prompt == nil {
		return nil, fmt.Errorf("%w: pending Ask payload is missing", ErrInvalidPromptAnswer)
	}
	questions := make(map[protocol.QuestionID]protocol.AskQuestion, len(prompt.Questions))
	for _, question := range prompt.Questions {
		questions[question.QuestionID] = question
	}
	seen := make(map[protocol.QuestionID]struct{}, len(answers))
	converted := make([]event.AskAnswer, 0, len(answers))
	for _, answer := range answers {
		question, exists := questions[answer.QuestionID]
		if !exists {
			return nil, fmt.Errorf("%w: unknown question ID %q", ErrInvalidPromptAnswer, answer.QuestionID)
		}
		if _, duplicate := seen[answer.QuestionID]; duplicate {
			return nil, fmt.Errorf("%w: duplicate question ID %q", ErrInvalidPromptAnswer, answer.QuestionID)
		}
		seen[answer.QuestionID] = struct{}{}
		if !question.Multi && len(answer.Selected) > 1 {
			return nil, fmt.Errorf("%w: single-select question %q has multiple selections", ErrInvalidPromptAnswer, answer.QuestionID)
		}
		optionLabels := make(map[string]struct{}, len(question.Options))
		for _, option := range question.Options {
			optionLabels[option.Label] = struct{}{}
		}
		selectedSeen := make(map[string]struct{}, len(answer.Selected))
		custom := 0
		selected := make([]string, len(answer.Selected))
		for index, value := range answer.Selected {
			if strings.TrimSpace(value) == "" {
				return nil, fmt.Errorf("%w: question %q has an empty selection", ErrInvalidPromptAnswer, answer.QuestionID)
			}
			if _, duplicate := selectedSeen[value]; duplicate {
				return nil, fmt.Errorf("%w: question %q repeats selection %q", ErrInvalidPromptAnswer, answer.QuestionID, value)
			}
			selectedSeen[value] = struct{}{}
			if _, known := optionLabels[value]; !known {
				custom++
			}
			selected[index] = value
		}
		// The frozen DTO represents a typed custom answer as one Selected
		// string. Mixing it with options, or sending two arbitrary strings,
		// cannot be distinguished faithfully by the Controller and is rejected.
		if custom > 0 && len(selected) != 1 {
			return nil, fmt.Errorf("%w: question %q mixes custom text with option selections", ErrInvalidPromptAnswer, answer.QuestionID)
		}
		converted = append(converted, event.AskAnswer{QuestionID: string(answer.QuestionID), Selected: selected})
	}
	return converted, nil
}

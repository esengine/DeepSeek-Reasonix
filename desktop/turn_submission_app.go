package main

import "reasonix/internal/event"

type turnSubmissionState struct {
	inFlight     bool
	submissionID string
}

func (t *WorkspaceTab) recordTurnStarted(now int64) int64 {
	t.telemMu.Lock()
	defer t.telemMu.Unlock()
	if t.usageTelemetry.activeTurnStartedAt == 0 {
		t.usageTelemetry.activeTurnStartedAt = now
	}
	return t.usageTelemetry.activeTurnStartedAt
}

func (t *WorkspaceTab) turnStartedAt() int64 {
	if t == nil {
		return 0
	}
	t.telemMu.Lock()
	defer t.telemMu.Unlock()
	return t.usageTelemetry.activeTurnStartedAt
}

// setBinding reroutes the sink while invalidating correlations that were
// created for a different frontend tab.
func (s *tabEventSink) setBinding(tabID string, app *App) {
	s.mu.Lock()
	if s.tabID != tabID {
		s.turn.submissionID = ""
	}
	s.tabID = tabID
	if app != nil {
		s.app = app
	}
	s.mu.Unlock()
}

func (s *tabEventSink) setRuntimeEpoch(epoch string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.runtimeEpoch != epoch {
		s.turn.submissionID = ""
	}
	s.runtimeEpoch = epoch
	s.mu.Unlock()
}

func (s *tabEventSink) clearContext() {
	s.mu.Lock()
	s.ctx = nil
	s.turn.submissionID = ""
	s.mu.Unlock()
	s.runtimeEvents.Clear()
}

func firstSubmissionID(ids []string) string {
	if len(ids) == 0 {
		return ""
	}
	return ids[0]
}

func (s *tabEventSink) submissionIDSnapshot() string {
	if s == nil {
		return ""
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.turn.submissionID
}

type correlatedWireEventTab struct {
	wireEventTab
	SubmissionID string `json:"submissionId,omitempty"`
}

func toWireTabWithSubmission(e event.Event, tabID, runtimeEpoch, submissionID string, turnStartedAt int64) any {
	wire := toWireTab(e, tabID, runtimeEpoch)
	if e.Kind == event.TurnStarted {
		wire.TurnStartedAt = turnStartedAt
	}
	if submissionID == "" {
		return wire
	}
	return correlatedWireEventTab{wireEventTab: wire, SubmissionID: submissionID}
}

// The WithID entry points correlate one optimistic desktop user item with the
// raw TurnDone produced by the turn that this call actually admits.
func (a *App) SubmitToTabWithID(tabID, input, submissionID string) error {
	if err := validateTurnInput(input); err != nil {
		return err
	}
	return a.submitToTab(tabID, input, false, submissionID)
}

func (a *App) SubmitDisplayToTabWithID(tabID, display, input, submissionID string) error {
	return a.submitDisplayToTab(tabID, display, input, submissionID)
}

func (a *App) submitDisplayToTab(tabID, display, input, submissionID string) error {
	if err := validateTurnInput(input); err != nil {
		return err
	}
	admission, ctrl, err := a.beginTabTurn(tabID, true, submissionID)
	if err != nil {
		return err
	}
	defer admission.abort()
	tab := admission.tab
	a.ensureTabTopicIndexedForUserTurn(tab)
	ctrl.SubmitDisplay(display, input)
	admission.finish(ctrl)
	return nil
}

func (a *App) SubmitDeliveryRecoveryToTabWithID(tabID, display, input, submissionID string) error {
	return a.submitDeliveryRecoveryToTab(tabID, display, input, submissionID)
}

func (a *App) submitDeliveryRecoveryToTab(tabID, display, input, submissionID string) error {
	if err := validateTurnInput(input); err != nil {
		return err
	}
	admission, ctrl, err := a.beginTabTurn(tabID, true, submissionID)
	if err != nil {
		return err
	}
	defer admission.abort()
	tab := admission.tab
	a.ensureTabTopicIndexedForUserTurn(tab)
	ctrl.SubmitDeliveryRecovery(display, input)
	admission.finish(ctrl)
	return nil
}

func (a *App) SubmitInvocationsToTabWithID(tabID, display, input string, invocations []InvocationRequest, submissionID string) error {
	return a.submitInvocationsToTab(tabID, display, input, invocations, submissionID)
}

func (a *App) submitInvocationsToTab(tabID, display, input string, invocations []InvocationRequest, submissionID string) error {
	if err := validateInvocationTurnInput(input, invocations); err != nil {
		return err
	}
	admission, ctrl, err := a.beginTabTurn(tabID, true, submissionID)
	if err != nil {
		return err
	}
	defer admission.abort()
	tab := admission.tab
	a.ensureTabTopicIndexedForUserTurn(tab)
	ctrl.SubmitInvocationDisplay(display, input, controlInvocationRequests(invocations))
	admission.finish(ctrl)
	return nil
}

func (a *App) SubmitInitialGoalToTabWithID(
	tabID, goal, display, input string,
	invocations []InvocationRequest,
	collaborationMode, toolApprovalMode, submissionID string,
) ([]string, error) {
	if err := validateInvocationTurnInput(input, invocations); err != nil {
		return []string{}, err
	}
	return a.submitInitialGoalToLocalTab(
		tabID, toolApprovalMode, goal, display, input, invocations, submissionID,
	)
}

func (a *App) SubmitEditedDisplayToTabWithID(tabID, display, input, original, submissionID string) error {
	return a.submitEditedDisplayToTab(tabID, display, input, original, submissionID)
}

func (a *App) submitEditedDisplayToTab(tabID, display, input, original, submissionID string) error {
	if err := validateTurnInput(input); err != nil {
		return err
	}
	admission, ctrl, err := a.beginTabTurn(tabID, true, submissionID)
	if err != nil {
		return err
	}
	defer admission.abort()
	tab := admission.tab
	a.ensureTabTopicIndexedForUserTurn(tab)
	ctrl.SubmitEditedDisplay(display, input, original)
	admission.finish(ctrl)
	return nil
}

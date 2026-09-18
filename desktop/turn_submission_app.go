package main

import (
	"errors"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"strings"
)

func (a *App) knownSubmission(tabID string, req control.SubmissionRequest) (bool, error) {
	tab, ctrl := a.tabAndCtrlByID(tabID)
	if a.tabIsReadOnly(tab) {
		return false, readOnlyChannelErr()
	}
	if identified, ok := ctrl.(*control.Controller); ok && req.ID != "" {
		_, found, err := identified.LookupSubmission(req)
		return found, err
	}
	return false, nil
}

// A competing retry may have waited behind the first caller's tab admission.
func (a *App) submissionAdmissionError(tabID string, req control.SubmissionRequest, err error) error {
	if errors.Is(err, control.ErrTurnRunning) {
		if found, lookupErr := a.knownSubmission(tabID, req); found || lookupErr != nil {
			return lookupErr
		}
	}
	return errors.Join(control.ErrSubmissionNotAccepted, err)
}

func submitIdentified(ctrl control.SessionAPI, req control.SubmissionRequest, submit func()) error {
	return submitIdentifiedWithSetup(ctrl, req, nil, submit)
}

func submitIdentifiedWithSetup(ctrl control.SessionAPI, req control.SubmissionRequest, setup func() error, submit func()) error {
	if identified, ok := ctrl.(*control.Controller); ok && identified.ClassifySubmitRoute(req.Input) != control.SubmitManagementHandled {
		if setup != nil {
			_, err := identified.SubmitIdentifiedWithSetup(req, setup)
			return inboxBridgeError(err)
		}
		_, err := identified.SubmitIdentified(req)
		return inboxBridgeError(err)
	}
	if setup != nil {
		if err := setup(); err != nil {
			return err
		}
	}
	submit()
	return nil
}

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
func (s *tabEventSink) setBinding(tabID string, app *App, generation ...uint64) {
	s.mu.Lock()
	if s.tabID != tabID {
		s.turn.submissionID = ""
	}
	s.tabID = tabID
	if len(generation) > 0 {
		if s.sessionGeneration != generation[0] {
			s.turn.submissionID = ""
		}
		s.sessionGeneration = generation[0]
	}
	if app != nil {
		s.app = app
	}
	s.mu.Unlock()
}

func (s *tabEventSink) setSessionGeneration(generation uint64) {
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.sessionGeneration != generation {
		s.turn.submissionID = ""
	}
	s.sessionGeneration = generation
	s.mu.Unlock()
}

func (s *tabEventSink) sessionGenerationSnapshot() uint64 {
	if s == nil {
		return 0
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.sessionGeneration
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

func toWireTabWithSubmission(e event.Event, tabID, runtimeEpoch, submissionID string, turnStartedAt int64, sessionGeneration ...uint64) any {
	wire := toWireTab(e, tabID, runtimeEpoch)
	if len(sessionGeneration) > 0 {
		wire.SessionGeneration = sessionGeneration[0]
	}
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
	req := control.SubmissionRequest{ID: submissionID, Input: input, Display: display}
	if found, err := a.knownSubmission(tabID, req); found || err != nil {
		return err
	}
	admission, ctrl, err := a.beginTabTurn(tabID, true, submissionID)
	if err != nil {
		return a.submissionAdmissionError(tabID, req, err)
	}
	defer admission.abort()
	tab := admission.tab
	if err := a.ensureTabTopicIndexedForUserTurn(tab); err != nil {
		return err
	}
	if err := submitIdentified(ctrl, req, func() { ctrl.SubmitDisplay(display, input) }); err != nil {
		return err
	}
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
	req := control.SubmissionRequest{ID: submissionID, Input: input, Display: display, Action: "delivery-recovery"}
	if found, err := a.knownSubmission(tabID, req); found || err != nil {
		return err
	}
	admission, ctrl, err := a.beginTabTurn(tabID, true, submissionID)
	if err != nil {
		return a.submissionAdmissionError(tabID, req, err)
	}
	defer admission.abort()
	tab := admission.tab
	if err := a.ensureTabTopicIndexedForUserTurn(tab); err != nil {
		return err
	}
	if err := submitIdentified(ctrl, req, func() { ctrl.SubmitDeliveryRecovery(display, input) }); err != nil {
		return err
	}
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
	req := control.SubmissionRequest{ID: submissionID, Input: input, Display: display, Invocations: controlInvocationRequests(invocations)}
	if found, err := a.knownSubmission(tabID, req); found || err != nil {
		return err
	}
	admission, ctrl, err := a.beginTabTurn(tabID, true, submissionID)
	if err != nil {
		return a.submissionAdmissionError(tabID, req, err)
	}
	defer admission.abort()
	tab := admission.tab
	if err := a.ensureTabTopicIndexedForUserTurn(tab); err != nil {
		return err
	}
	if err := submitIdentified(ctrl, req, func() { ctrl.SubmitInvocationDisplay(display, input, controlInvocationRequests(invocations)) }); err != nil {
		return err
	}
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
	req := control.SubmissionRequest{ID: submissionID, Input: input, Display: display, Original: original}
	if found, err := a.knownSubmission(tabID, req); found || err != nil {
		return err
	}
	admission, ctrl, err := a.beginTabTurn(tabID, true, submissionID)
	if err != nil {
		return a.submissionAdmissionError(tabID, req, err)
	}
	defer admission.abort()
	tab := admission.tab
	if err := a.ensureTabTopicIndexedForUserTurn(tab); err != nil {
		return err
	}
	if err := submitIdentified(ctrl, req, func() { ctrl.SubmitEditedDisplay(display, input, original) }); err != nil {
		return err
	}
	admission.finish(ctrl)
	return nil
}

func (a *App) submitToTabResult(tabID, input string, fromBridge, classifyManagement bool, submissionID ...string) (control.SubmitResult, error) {
	if found, err := a.knownSubmission(tabID, control.SubmissionRequest{ID: firstSubmissionID(submissionID), Input: input, Display: input}); found || err != nil {
		return control.SubmitResult{Disposition: control.SubmitTurnStarted}, err
	}
	management := control.SubmitResult{Disposition: control.SubmitManagementHandled}
	trimmed := strings.TrimSpace(input)
	if trimmed == "/reload" {
		return a.submitReloadCommand(tabID, fromBridge)
	}
	if trimmed == "/effort" || strings.HasPrefix(trimmed, "/effort ") {
		tab, _ := a.tabAndCtrlByID(tabID)
		if a.tabIsReadOnly(tab) {
			return control.SubmitResult{}, readOnlyChannelErr()
		}
		if tab == nil {
			return control.SubmitResult{}, a.workspaceNotReadyErr(tab)
		}
		if !fromBridge && a.botBridge != nil {
			a.botBridge.reclaimFromDesktop(tab.ID)
		}
		a.runEffortCommandForTab(tabID, trimmed)
		return management, nil
	}
	if classifyManagement {
		tab, ctrl := a.tabAndCtrlByID(tabID)
		if a.tabIsReadOnly(tab) {
			return control.SubmitResult{}, readOnlyChannelErr()
		}
		if err := a.workspaceRuntimeAdmissionErr(tab, ctrl); err != nil {
			return control.SubmitResult{}, err
		}
		if err := a.ensureTabControllerWorkspace(tab); err != nil {
			return control.SubmitResult{}, err
		}
		ctrl = a.controllerForTab(tab)
		if ctrl == nil {
			return control.SubmitResult{}, a.workspaceNotReadyErr(tab)
		}
		managementRoute := false
		if classifier, ok := ctrl.(interface {
			ClassifySubmitRoute(input string) control.SubmitDisposition
		}); ok {
			managementRoute = classifier.ClassifySubmitRoute(input) == control.SubmitManagementHandled
		}
		if managementRoute {
			// Management commands still take the tab admission lock so they cannot
			// race an active turn or a controller replacement.
			admission, admittedCtrl, err := a.beginTabTurn(tabID, !fromBridge, submissionID...)
			if err != nil {
				return control.SubmitResult{}, err
			}
			defer admission.abort()
			tab = admission.tab
			if err := a.ensureTabTopicIndexedForUserTurn(tab); err != nil {
				return control.SubmitResult{}, err
			}
			if submitter, supported := admittedCtrl.(interface {
				SubmitDisplayWithResult(display, input string) control.SubmitResult
			}); supported {
				result := submitter.SubmitDisplayWithResult(input, input)
				admission.finish(admittedCtrl)
				return result, nil
			}
			admittedCtrl.SubmitDisplay(input, input)
			admission.finish(admittedCtrl)
			return management, nil
		}
	}
	admission, ctrl, err := a.beginTabTurn(tabID, !fromBridge, submissionID...)
	if err != nil {
		return control.SubmitResult{Disposition: control.SubmitTurnStarted}, a.submissionAdmissionError(tabID,
			control.SubmissionRequest{ID: firstSubmissionID(submissionID), Input: input, Display: input}, err)
	}
	defer admission.abort()
	tab := admission.tab
	if err := a.ensureTabTopicIndexedForUserTurn(tab); err != nil {
		return control.SubmitResult{}, err
	}
	result := control.SubmitResult{Disposition: control.SubmitTurnStarted}
	if identified, ok := ctrl.(*control.Controller); ok && firstSubmissionID(submissionID) != "" && identified.ClassifySubmitRoute(input) != control.SubmitManagementHandled {
		_, err := identified.SubmitIdentified(control.SubmissionRequest{ID: firstSubmissionID(submissionID), Input: input, Display: input})
		if err != nil {
			return control.SubmitResult{}, err
		}
	} else if submitter, ok := ctrl.(interface {
		SubmitDisplayWithResult(display, input string) control.SubmitResult
	}); ok {
		result = submitter.SubmitDisplayWithResult(input, input)
	} else {
		ctrl.SubmitDisplay(input, input)
	}
	admission.finish(ctrl)
	return result, nil
}

func (a *App) submitReloadCommand(tabID string, fromBridge bool) (control.SubmitResult, error) {
	management := control.SubmitResult{Disposition: control.SubmitManagementHandled}
	tab, _ := a.tabAndCtrlByID(tabID)
	if a.tabIsReadOnly(tab) {
		return control.SubmitResult{}, readOnlyChannelErr()
	}
	if tab == nil {
		return control.SubmitResult{}, a.workspaceNotReadyErr(tab)
	}
	if !fromBridge && a.botBridge != nil {
		a.botBridge.reclaimFromDesktop(tab.ID)
	}
	return management, a.ReloadRuntime(tab.ID)
}

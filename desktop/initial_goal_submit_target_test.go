package main

import (
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/control"
	"reasonix/internal/remote/workbench/target"
)

type initialGoalSubmitRecorder struct {
	control.SessionAPI
	display     string
	input       string
	invocations []control.InvocationRequest
}

func (r *initialGoalSubmitRecorder) SubmitDisplay(display, input string) {
	r.display = display
	r.input = input
}

func (r *initialGoalSubmitRecorder) SubmitInvocationDisplay(
	display, input string,
	invocations []control.InvocationRequest,
) {
	r.display = display
	r.input = input
	r.invocations = append([]control.InvocationRequest(nil), invocations...)
}

func TestSubmitInitialGoalRejectsStaleRemoteTargetWithoutTouchingLocalTab(t *testing.T) {
	app := testAppWithOrderedTabs(t, "a", "a")
	ctrl := control.New(control.Options{Label: "test"})
	defer ctrl.Close()
	tab := app.tabs["a"]
	tab.Ctrl = ctrl
	tab.goal = "existing local goal"
	ctrl.SetGoal(tab.goal)

	_, remoteGen, err := app.workbench().targets.BeginRemoteConnect("remote-host", "/srv/work")
	if err != nil {
		t.Fatal(err)
	}
	if err := app.workbench().targets.MarkRemoteConnected(remoteGen); err != nil {
		t.Fatal(err)
	}
	active, identityGen, requestSeq, err := app.workbench().targets.ActivateRemote(remoteGen)
	if err != nil {
		t.Fatal(err)
	}
	if active.Kind != target.KindRemote {
		t.Fatalf("active target = %+v, want Remote", active)
	}
	app.workbench().targets.SwitchLocal()

	beforeControllerGoal := ctrl.Goal()
	beforeGoalStatus := ctrl.GoalStatus()
	beforeHistoryLen := len(ctrl.History())
	_, err = app.SubmitInitialGoalToTab(
		tab.ID,
		"replacement goal",
		"/ui-ux-pro-max replacement goal",
		"replacement goal",
		[]InvocationRequest{{Name: "ui-ux-pro-max", Kind: "skill"}},
		"normal",
		"ask",
		string(active.Kind),
		identityGen,
		requestSeq,
	)
	if err == nil || !strings.Contains(err.Error(), "Execution target changed") {
		t.Fatalf("SubmitInitialGoalToTab error = %v, want target-changed error", err)
	}
	if tab.goal != "existing local goal" {
		t.Fatalf("local tab goal = %q, want unchanged", tab.goal)
	}
	if got := ctrl.Goal(); got != beforeControllerGoal {
		t.Fatalf("local controller goal = %q, want unchanged %q", got, beforeControllerGoal)
	}
	if got := ctrl.GoalStatus(); got != beforeGoalStatus {
		t.Fatalf("local controller goal status = %q, want unchanged %q", got, beforeGoalStatus)
	}
	if got := len(ctrl.History()); got != beforeHistoryLen {
		t.Fatalf("local controller history len = %d, want unchanged %d", got, beforeHistoryLen)
	}
}

func TestSubmitInitialGoalAcceptsCurrentLocalTarget(t *testing.T) {
	app := testAppWithOrderedTabs(t, "a", "a")
	ctrl := control.New(control.Options{Label: "test"})
	defer ctrl.Close()
	recorder := &initialGoalSubmitRecorder{SessionAPI: ctrl}
	tab := app.tabs["a"]
	tab.Ctrl = recorder

	active, identityGen, requestSeq := app.workbench().targets.Active()
	_, err := app.SubmitInitialGoalToTab(
		tab.ID,
		"ship the fix",
		"/ui-ux-pro-max ship the fix",
		"ship the fix",
		[]InvocationRequest{{Name: "ui-ux-pro-max", Kind: "skill", Offset: 4}},
		"normal",
		"ask",
		string(active.Kind),
		identityGen,
		requestSeq,
	)
	if err != nil {
		t.Fatal(err)
	}
	if tab.goal != "ship the fix" {
		t.Fatalf("tab goal = %q, want %q", tab.goal, "ship the fix")
	}
	if got := ctrl.Goal(); got != "ship the fix" {
		t.Fatalf("controller goal = %q, want %q", got, "ship the fix")
	}
	if recorder.display != "/ui-ux-pro-max ship the fix" || recorder.input != "ship the fix" {
		t.Fatalf("recorded submit = display %q input %q", recorder.display, recorder.input)
	}
	if len(recorder.invocations) != 1 {
		t.Fatalf("recorded invocations = %+v, want one", recorder.invocations)
	}
	if got := recorder.invocations[0]; got.Name != "ui-ux-pro-max" || got.Kind != "skill" || got.Offset != 4 {
		t.Fatalf("recorded invocation = %+v", got)
	}
}

func TestSubmitInitialGoalAppliesToolApprovalProfileBeforeSubmit(t *testing.T) {
	app := testAppWithOrderedTabs(t, "a", "a")
	ctrl := control.New(control.Options{Label: "test"})
	defer ctrl.Close()
	recorder := &initialGoalSubmitRecorder{SessionAPI: ctrl}
	tab := app.tabs["a"]
	tab.Ctrl = recorder

	active, identityGen, requestSeq := app.workbench().targets.Active()
	_, err := app.SubmitInitialGoalToTab(
		tab.ID,
		"ship with YOLO",
		"ship with YOLO",
		"ship with YOLO",
		nil,
		"normal",
		string(control.ToolApprovalYolo),
		string(active.Kind),
		identityGen,
		requestSeq,
	)
	if err != nil {
		t.Fatal(err)
	}
	if tab.toolApprovalMode != string(control.ToolApprovalYolo) {
		t.Fatalf("tab approval mode = %q, want %q", tab.toolApprovalMode, control.ToolApprovalYolo)
	}
	if got := recorder.ToolApprovalMode(); got != string(control.ToolApprovalYolo) {
		t.Fatalf("controller approval mode = %q, want %q", got, control.ToolApprovalYolo)
	}
	if recorder.input != "ship with YOLO" {
		t.Fatalf("recorded input = %q, want first Goal input", recorder.input)
	}
}

func TestWorkbenchTargetEventKeepsZeroRequestSequence(t *testing.T) {
	payload, err := json.Marshal(WorkbenchTargetStateView{
		State:       "disconnected",
		Kind:        target.KindLocal,
		IdentityGen: 1,
		RequestSeq:  0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(payload), `"requestSeq":0`) {
		t.Fatalf("target event payload = %s, want explicit zero requestSeq", payload)
	}
}

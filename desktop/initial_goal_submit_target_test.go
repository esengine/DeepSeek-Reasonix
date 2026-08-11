package main

import (
	"testing"

	"reasonix/internal/control"
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

func TestSubmitInitialGoalAcceptsCurrentLocalTarget(t *testing.T) {
	app := testAppWithOrderedTabs(t, "a", "a")
	ctrl := control.New(control.Options{Label: "test"})
	defer ctrl.Close()
	recorder := &initialGoalSubmitRecorder{SessionAPI: ctrl}
	tab := app.tabs["a"]
	tab.Ctrl = recorder

	_, err := app.SubmitInitialGoalToTab(
		tab.ID,
		"ship the fix",
		"/ui-ux-pro-max ship the fix",
		"ship the fix",
		[]InvocationRequest{{Name: "ui-ux-pro-max", Kind: "skill", Offset: 4}},
		"normal",
		"ask",
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

	_, err := app.SubmitInitialGoalToTab(
		tab.ID,
		"ship with YOLO",
		"ship with YOLO",
		"ship with YOLO",
		nil,
		"normal",
		string(control.ToolApprovalYolo),
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

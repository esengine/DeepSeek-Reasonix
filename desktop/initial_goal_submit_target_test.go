package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/control"
)

type initialGoalSubmitRecorder struct {
	control.SessionAPI
	display     string
	input       string
	invocations []control.InvocationRequest
}

func TestSubmitInitialGoalStopsBeforeProviderWhenPersistenceFails(t *testing.T) {
	app := testAppWithOrderedTabs(t, "a", "a")
	base := control.New(control.Options{Label: "test"})
	defer base.Close()
	recorder := &initialGoalSubmitRecorder{SessionAPI: base}
	tab := app.tabs["a"]
	tab.Ctrl = &rejectingGoalSession{SessionAPI: recorder, err: errors.New("disk full")}

	_, err := app.SubmitInitialGoalToTab(
		tab.ID, "ship the fix", "ship the fix", "ship the fix", nil,
		"normal", "ask",
	)
	if err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("SubmitInitialGoalToTab error = %v, want persistence failure", err)
	}
	if recorder.input != "" || recorder.display != "" {
		t.Fatalf("provider submission escaped failed goal mutation: display=%q input=%q", recorder.display, recorder.input)
	}
	if tab.goal != "" {
		t.Fatalf("tab goal = %q, want unpublished", tab.goal)
	}
}

func TestSubmitInitialGoalRejectsMissingImageBeforeGoalMutation(t *testing.T) {
	workspace := t.TempDir()
	ctrl := control.New(control.Options{WorkspaceRoot: workspace})
	t.Cleanup(ctrl.Close)
	tab := &WorkspaceTab{
		ID:            "a",
		Scope:         "project",
		WorkspaceRoot: workspace,
		TopicID:       "topic_a",
		TopicTitle:    "A",
		Ready:         true,
		Ctrl:          ctrl,
		disabledMCP:   map[string]ServerView{},
	}
	app := &App{tabs: map[string]*WorkspaceTab{"a": tab}, tabOrder: []string{"a"}, activeTabID: "a"}

	_, err := app.SubmitInitialGoalToTab(
		tab.ID,
		"inspect the image",
		"inspect missing.png",
		"inspect @.reasonix/attachments/missing.png",
		nil,
		"normal",
		"ask",
	)
	if err == nil || err.Error() != "reasonix_error:image_attachment_unreadable" {
		t.Fatalf("error = %v, want stable image failure", err)
	}
	if tab.goal != "" || ctrl.Goal() != "" {
		t.Fatalf("rejected image mutated goal: tab=%q controller=%q", tab.goal, ctrl.Goal())
	}
	if ctrl.Running() {
		t.Fatal("rejected image started a turn")
	}
	if _, statErr := os.Stat(filepath.Join(workspace, ".reasonix", "attachments")); !os.IsNotExist(statErr) {
		t.Fatalf("failed image read created attachment directory: %v", statErr)
	}
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

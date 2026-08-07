package main

import (
	"context"
	"errors"
	"testing"

	"reasonix/internal/control"
)

// extensionUIController fakes the extension-UI slice of control.SessionAPI:
// the Capabilities port methods plus the concrete controller's
// SubmitExtensionForm (asserted locally by the binding until the port grows
// it).
type extensionUIController struct {
	control.SessionAPI
	actions         []control.ExtensionActionView
	invokeMessage   string
	invokeErr       error
	submitErr       error
	invokeName      string
	invokeArgs      map[string]string
	submitPluginID  string
	submitSurfaceID string
	submitValues    map[string]any
}

func (c *extensionUIController) ExtensionActions() []control.ExtensionActionView {
	return append([]control.ExtensionActionView(nil), c.actions...)
}

func (c *extensionUIController) InvokeExtensionAction(_ context.Context, name string, args map[string]string) (string, error) {
	c.invokeName = name
	c.invokeArgs = args
	return c.invokeMessage, c.invokeErr
}

func (c *extensionUIController) SubmitExtensionForm(_ context.Context, pluginID, surfaceID string, values map[string]any) error {
	c.submitPluginID = pluginID
	c.submitSurfaceID = surfaceID
	c.submitValues = values
	return c.submitErr
}

func newExtensionTestApp(ctrl control.SessionAPI) *App {
	return &App{
		tabs: map[string]*WorkspaceTab{
			"target":  {ID: "target", Scope: "global", Ready: true, Ctrl: ctrl, disabledMCP: map[string]ServerView{}},
			"focused": {ID: "focused", Scope: "global", Ready: true, disabledMCP: map[string]ServerView{}},
		},
		tabOrder:    []string{"target", "focused"},
		activeTabID: "focused",
	}
}

func TestExtensionActionsMapsControlViewsAndRoutesByTab(t *testing.T) {
	isolateDesktopUserDirs(t)
	targetCtrl := &extensionUIController{actions: []control.ExtensionActionView{
		{PluginID: "alpha", ActionID: "act1", Label: "Run the thing", Slash: "/alpha:act1"},
		{PluginID: "beta", ActionID: "act2", Label: "", Slash: "/beta:act2"},
	}}
	app := newExtensionTestApp(targetCtrl)

	views, err := app.ExtensionActions("target")
	if err != nil {
		t.Fatalf("ExtensionActions: %v", err)
	}
	if len(views) != 2 {
		t.Fatalf("ExtensionActions = %+v, want 2 views", views)
	}
	if views[0] != (ExtensionActionView{Plugin: "alpha", Action: "act1", Slash: "/alpha:act1", Description: "Run the thing"}) {
		t.Fatalf("view[0] = %+v, want JSON twin of the control view", views[0])
	}
	if views[1].Description != "" {
		t.Fatalf("empty label must stay an omitted description, got %+v", views[1])
	}

	// Empty tab id resolves the active tab.
	active, err := app.ExtensionActions("")
	if err != nil {
		t.Fatalf("ExtensionActions(active): %v", err)
	}
	if len(active) != 0 {
		t.Fatalf("active tab actions = %+v, want empty (focused tab has none)", active)
	}

	// Unknown tabs and tabs without a running runtime yield an empty list.
	for _, tabID := range []string{"missing", "focused-noruntime"} {
		app.tabs["focused-noruntime"] = &WorkspaceTab{ID: "focused-noruntime", Scope: "global", disabledMCP: map[string]ServerView{}}
		got, err := app.ExtensionActions(tabID)
		if err != nil {
			t.Fatalf("ExtensionActions(%q): %v", tabID, err)
		}
		if len(got) != 0 {
			t.Fatalf("ExtensionActions(%q) = %+v, want empty", tabID, got)
		}
	}
}

func TestInvokeExtensionActionForwardsNameAndArgs(t *testing.T) {
	isolateDesktopUserDirs(t)
	targetCtrl := &extensionUIController{invokeMessage: "done"}
	focusedCtrl := &extensionUIController{}
	app := newExtensionTestApp(targetCtrl)

	message, err := app.InvokeExtensionAction("target", "/alpha:act1", map[string]string{"k": "v"})
	if err != nil {
		t.Fatalf("InvokeExtensionAction: %v", err)
	}
	if message != "done" {
		t.Fatalf("message = %q, want done", message)
	}
	if targetCtrl.invokeName != "/alpha:act1" || targetCtrl.invokeArgs["k"] != "v" {
		t.Fatalf("controller saw name=%q args=%+v", targetCtrl.invokeName, targetCtrl.invokeArgs)
	}
	if focusedCtrl.invokeName != "" {
		t.Fatalf("focused tab received scoped invoke: %q", focusedCtrl.invokeName)
	}

	targetCtrl.invokeErr = errors.New("sidecar said no")
	if _, err := app.InvokeExtensionAction("target", "/alpha:act1", nil); err == nil || err.Error() != "sidecar said no" {
		t.Fatalf("controller error must pass through, got %v", err)
	}

	if _, err := app.InvokeExtensionAction("target", "  ", nil); err == nil {
		t.Fatal("blank action name must fail before reaching the controller")
	}
	if _, err := app.InvokeExtensionAction("missing", "/alpha:act1", nil); err == nil {
		t.Fatal("unknown tab must fail")
	}
	if _, err := app.InvokeExtensionAction("focused-noruntime", "/alpha:act1", nil); err == nil {
		t.Fatal("tab without a running runtime must fail")
	}
}

func TestSubmitExtensionFormRoutesValues(t *testing.T) {
	isolateDesktopUserDirs(t)
	targetCtrl := &extensionUIController{}
	app := newExtensionTestApp(targetCtrl)

	values := map[string]any{"name": "x", "flags": []any{"a", "b"}, "agree": true}
	if err := app.SubmitExtensionForm("target", "alpha", "f1", values); err != nil {
		t.Fatalf("SubmitExtensionForm: %v", err)
	}
	if targetCtrl.submitPluginID != "alpha" || targetCtrl.submitSurfaceID != "f1" {
		t.Fatalf("controller saw %q/%q", targetCtrl.submitPluginID, targetCtrl.submitSurfaceID)
	}
	if targetCtrl.submitValues["name"] != "x" || targetCtrl.submitValues["agree"] != true {
		t.Fatalf("values = %+v", targetCtrl.submitValues)
	}

	// A dismissal rides the same path as an explicit cancelled value.
	if err := app.SubmitExtensionForm("target", "alpha", "f1", map[string]any{"cancelled": true}); err != nil {
		t.Fatalf("SubmitExtensionForm(cancel): %v", err)
	}
	if targetCtrl.submitValues["cancelled"] != true {
		t.Fatalf("cancelled value = %+v", targetCtrl.submitValues)
	}

	targetCtrl.submitErr = errors.New("not accepted")
	if err := app.SubmitExtensionForm("target", "alpha", "f1", nil); err == nil || err.Error() != "not accepted" {
		t.Fatalf("controller error must pass through, got %v", err)
	}

	if err := app.SubmitExtensionForm("target", "", "f1", nil); err == nil {
		t.Fatal("blank plugin id must fail before reaching the controller")
	}
	if err := app.SubmitExtensionForm("target", "alpha", "", nil); err == nil {
		t.Fatal("blank surface id must fail before reaching the controller")
	}
	if err := app.SubmitExtensionForm("missing", "alpha", "f1", nil); err == nil {
		t.Fatal("unknown tab must fail")
	}
}

// SubmitExtensionForm is not on control.SessionAPI yet; a controller that only
// satisfies the port must produce a clean error instead of a panic.
func TestSubmitExtensionFormWithoutSubmitterFailsCleanly(t *testing.T) {
	isolateDesktopUserDirs(t)
	app := newExtensionTestApp(&extensionUIActionsOnlyController{})
	if err := app.SubmitExtensionForm("target", "alpha", "f1", nil); err == nil {
		t.Fatal("SubmitExtensionForm without a submitter must fail")
	}
}

type extensionUIActionsOnlyController struct {
	control.SessionAPI
}

func (c *extensionUIActionsOnlyController) ExtensionActions() []control.ExtensionActionView {
	return nil
}

package main

import (
	"context"
	"fmt"
	"strings"

	"reasonix/internal/control"
)

// ExtensionActionView is the JSON twin of control.ExtensionActionView for the
// desktop frontend: one handshake-declared extension UI action. Description
// carries the control view's Label — the protocol declares a single
// human-readable label per action, so the frontend shows it as the action's
// description line. Slash is the public invocation name, "/<plugin>:<action>".
type ExtensionActionView struct {
	Plugin      string `json:"plugin"`
	Action      string `json:"action"`
	Slash       string `json:"slash"`
	Description string `json:"description,omitempty"`
}

// extensionFormSubmitter is the slice of *control.Controller that delivers a
// form surface's values back to the owning sidecar. control.SessionAPI does
// not expose SubmitExtensionForm yet (stage 8a added it to the concrete
// controller only; the Capabilities port carries just ExtensionActions and
// InvokeExtensionAction), so the desktop binding asserts it locally, mirroring
// cancelJobForController's CancelJob assertion. If the port grows the method,
// this alias can go away.
type extensionFormSubmitter interface {
	SubmitExtensionForm(ctx context.Context, pluginID, surfaceID string, values map[string]any) error
}

// extensionActionsForCtrl maps the control port's action views to their JSON
// twins. Nil controller → empty, so the palette simply shows no extension
// group while a tab's runtime is still starting.
func extensionActionsForCtrl(ctrl control.SessionAPI) []ExtensionActionView {
	out := []ExtensionActionView{}
	if ctrl == nil {
		return out
	}
	for _, action := range ctrl.ExtensionActions() {
		out = append(out, ExtensionActionView{
			Plugin:      action.PluginID,
			Action:      action.ActionID,
			Slash:       action.Slash,
			Description: action.Label,
		})
	}
	return out
}

// ExtensionActions enumerates the handshake-declared extension UI actions of
// the runtime owning tabID (empty tabID = active tab). Unknown tabs and tabs
// without a running runtime yield an empty list rather than an error so the
// command palette can poll freely.
func (a *App) ExtensionActions(tabID string) ([]ExtensionActionView, error) {
	return extensionActionsForCtrl(a.ctrlByTabID(tabID)), nil
}

// extensionCtrlForTab resolves the driving controller for extension calls,
// erroring instead of silently no-opping (unlike the read path above, invoke
// and submit are user-initiated mutations that must surface misrouting).
func (a *App) extensionCtrlForTab(tabID string) (control.SessionAPI, error) {
	if a.tabByID(tabID) == nil {
		return nil, fmt.Errorf("no tab %q", tabID)
	}
	ctrl := a.ctrlByTabID(tabID)
	if ctrl == nil {
		return nil, fmt.Errorf("tab %q has no running runtime", tabID)
	}
	return ctrl, nil
}

// InvokeExtensionAction invokes one registered extension action by its public
// "/<plugin>:<action>" name on behalf of tabID (empty = active tab) and
// returns the extension's (already redacted) result message.
func (a *App) InvokeExtensionAction(tabID, name string, args map[string]string) (string, error) {
	if strings.TrimSpace(name) == "" {
		return "", fmt.Errorf("extension action name is required")
	}
	ctrl, err := a.extensionCtrlForTab(tabID)
	if err != nil {
		return "", err
	}
	return ctrl.InvokeExtensionAction(a.bootContext(), name, args)
}

// SubmitExtensionForm delivers one extension form surface's values back to the
// owning sidecar. A dismissal rides the same path as values{"cancelled": true}
// — the structured-UI protocol has no dedicated cancel verb on the submit
// channel, so the frontend reports cancellation as an explicit value, distinct
// from an empty value set.
func (a *App) SubmitExtensionForm(tabID, pluginID, surfaceID string, values map[string]any) error {
	if strings.TrimSpace(pluginID) == "" || strings.TrimSpace(surfaceID) == "" {
		return fmt.Errorf("extension form submission requires plugin and surface ids")
	}
	ctrl, err := a.extensionCtrlForTab(tabID)
	if err != nil {
		return err
	}
	submitter, ok := ctrl.(extensionFormSubmitter)
	if !ok {
		return fmt.Errorf("extension form submission is unavailable on this runtime")
	}
	return submitter.SubmitExtensionForm(a.bootContext(), pluginID, surfaceID, values)
}

package control

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"reasonix/internal/event"
	"reasonix/internal/extension/uihub"
)

// Extension UI hub wiring (Extension Protocol v2, stage 8a). The hub is the
// host side of the extension structured-UI surface: sidecar publications
// arrive as events through EmitExtensionEvent, blocking prompts ride the
// ordinary Ask channel, and handshake-declared actions are exposed to
// frontends through the Capabilities port as /<plugin>:<action> names. Every
// wiring point is nil-safe: with no hub installed (no v2 runtime packages)
// the controller behaves exactly as before.

// ExtensionActionView is one handshake-declared extension UI action for
// frontend enumeration. Slash is the public invocation name,
// "/<plugin>:<action>".
type ExtensionActionView struct {
	PluginID string
	ActionID string
	Label    string
	Slash    string
}

// SetExtensionUI installs the extension UI hub after construction. Boot uses
// it because sidecars — and therefore the hub — only exist after snapshot
// assembly, which runs after New. The first non-nil install wins; a
// controller generation never swaps hubs. Nil is a no-op.
func (c *Controller) SetExtensionUI(h *uihub.Hub) {
	if h == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.extensionUI != nil {
		return
	}
	c.extensionUI = h
}

// extensionUIHub returns the installed hub, or nil.
func (c *Controller) extensionUIHub() *uihub.Hub {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.extensionUI
}

// EmitExtensionEvent emits one extension-sourced event to the controller's
// sink. The hub calls it for host/ui/publish traffic; reading the sink under
// lock keeps the emission race-free against SetExtensions installing the
// frontend-event strategy sink during boot.
func (c *Controller) EmitExtensionEvent(ev event.Event) {
	c.mu.Lock()
	sink := c.sink
	c.mu.Unlock()
	if sink == nil {
		return
	}
	sink.Emit(ev)
}

// ExtensionActions enumerates every registered extension UI action (the
// Capabilities port addition consumed by the stage-8b slash dispatch). Nil
// hub → empty.
func (c *Controller) ExtensionActions() []ExtensionActionView {
	h := c.extensionUIHub()
	if h == nil {
		return nil
	}
	registered := h.Actions()
	out := make([]ExtensionActionView, 0, len(registered))
	for _, action := range registered {
		out = append(out, ExtensionActionView{
			PluginID: action.PluginID,
			ActionID: action.ActionID,
			Label:    action.Label,
			Slash:    action.Slash,
		})
	}
	return out
}

// InvokeExtensionAction invokes one registered action by its public
// "/<plugin>:<action>" name (or bare "<plugin>:<action>") and returns the
// extension's (already redacted) result message.
func (c *Controller) InvokeExtensionAction(ctx context.Context, name string, args map[string]string) (string, error) {
	h := c.extensionUIHub()
	if h == nil {
		return "", errors.New("no extension UI hub is installed (no extension runtimes started)")
	}
	pluginID, actionID, ok := uihub.ParseSlashName(name)
	if !ok {
		return "", fmt.Errorf("invalid extension action name %q: want /<plugin>:<action>", name)
	}
	result, err := h.InvokeAction(ctx, pluginID, actionID, h.SessionID(), args)
	if err != nil {
		return "", err
	}
	if !result.Accepted {
		if result.Message != "" {
			return "", errors.New(result.Message)
		}
		return "", fmt.Errorf("extension %s did not accept action %s", pluginID, actionID)
	}
	return result.Message, nil
}

// SubmitExtensionForm delivers one extension form surface's values back to
// the owning sidecar (stage-8b frontends call it when a form is submitted).
func (c *Controller) SubmitExtensionForm(ctx context.Context, pluginID, surfaceID string, values map[string]any) error {
	h := c.extensionUIHub()
	if h == nil {
		return errors.New("no extension UI hub is installed (no extension runtimes started)")
	}
	result, err := h.Submit(ctx, pluginID, surfaceID, h.SessionID(), values)
	if err != nil {
		return err
	}
	if !result.Accepted {
		return fmt.Errorf("extension %s did not accept the submission for surface %s", pluginID, surfaceID)
	}
	return nil
}

// ParseExtensionActionArgs maps the trailing fields of a
// "/<plugin>:<action> args…" invocation onto the action's string map:
// key=value fields become named entries, bare fields land in positional
// arg1..argN keys. Both stage-8b frontends (TUI slash dispatch, ACP slash
// resolution) share this one parsing convention.
func ParseExtensionActionArgs(fields []string) map[string]string {
	if len(fields) == 0 {
		return nil
	}
	args := map[string]string{}
	positional := 0
	for _, field := range fields {
		if k, v, ok := strings.Cut(field, "="); ok && k != "" {
			args[k] = v
			continue
		}
		positional++
		args[fmt.Sprintf("arg%d", positional)] = field
	}
	return args
}

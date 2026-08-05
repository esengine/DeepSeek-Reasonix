package navigator

import (
	"context"
	"encoding/json"
	"fmt"

	"reasonix/internal/event"
	"reasonix/internal/tool"
)

// ReasonixAdapter bridges the Navigator Kernel to Reasonix's existing
// agent/control/hook/event stack. It is the production adapter; HermesAdapter
// is the HERMES counterpart.
//
// Design note: the adapter dispatches through tool.Registry.Execute, which is
// the same path the agent's run loop uses. The agent's own CallResolver,
// permission gate, Pre/PostToolUse hooks, and evidence recording stay in the
// agent layer — the navigator is a state-continuity + closed-loop-correction
// layer that sits alongside the security layer, not a replacement for it.
//
// Integration: boot.go constructs this adapter with the real registry + sink,
// plus optional callbacks for permission probing and environment/interface
// snapshotting. The agent's run loop calls navigator.Execute(action) around
// each tool call; the adapter dispatches the real tool and the navigator
// observes the result, tracks state, and corrects on deviation.
type ReasonixAdapter struct {
	registry *tool.Registry
	sink     event.Sink

	// permCheck is an optional callback that mirrors the agent's permission
	// gate. When nil, Permission returns true (the agent's own gate is the
	// authoritative check; the navigator's pre-check is advisory).
	permCheck func(ctx context.Context, verb, args string) (allowed bool, reason string)

	// ifaceProbe captures the current UI state for hashing. When nil, the
	// navigator's interface hash is empty (no interface drift detection).
	ifaceProbe func(ctx context.Context) (string, error)

	// envProbe captures the current environment digest. When nil, the
	// navigator falls back to the FilesystemSensor.
	envProbe func(ctx context.Context) (string, error)
}

// ReasonixAdapterOptions holds the injectable callbacks so boot.go can wire
// the adapter to the real permission gate and probes without the navigator
// package importing agent-specific permission types.
type ReasonixAdapterOptions struct {
	PermissionCheck func(ctx context.Context, verb, args string) (bool, string)
	InterfaceProbe  func(ctx context.Context) (string, error)
	EnvProbe        func(ctx context.Context) (string, error)
}

// NewReasonixAdapter creates the production adapter. registry and sink are
// required; opts carries optional callbacks.
func NewReasonixAdapter(registry *tool.Registry, sink event.Sink, opts ReasonixAdapterOptions) *ReasonixAdapter {
	return &ReasonixAdapter{
		registry:   registry,
		sink:       sink,
		permCheck:  opts.PermissionCheck,
		ifaceProbe: opts.InterfaceProbe,
		envProbe:   opts.EnvProbe,
	}
}

// Execute dispatches a HostAction through Reasonix's tool registry. The Verb
// field is the tool name (e.g. "Read", "Write", "Bash"); the Args field is the
// raw JSON arguments string.
func (a *ReasonixAdapter) Execute(ctx context.Context, action HostAction) (HostResult, error) {
	if a.registry == nil {
		return HostResult{}, fmt.Errorf("navigator: reasonix adapter has no tool registry")
	}
	t, ok := a.registry.Get(action.Verb)
	if !ok {
		return HostResult{Err: fmt.Errorf("navigator: tool %q not found in registry", action.Verb)}, fmt.Errorf("tool %q not found", action.Verb)
	}
	args := json.RawMessage(action.Args)
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}
	// ImageTool-aware dispatch: if the tool can return images, use the image
	// path so the navigator doesn't strip image data from the result text.
	if imgTool, ok := t.(tool.ImageTool); ok {
		text, _, err := imgTool.ExecuteWithImages(ctx, args)
		return HostResult{Output: text, Err: err}, err
	}
	out, err := t.Execute(ctx, args)
	return HostResult{Output: out, Err: err}, err
}

// Permission checks whether the action would be allowed. When no permCheck
// callback is wired, it returns true (advisory) — the agent's real gate is
// the authoritative check and runs in the agent layer.
func (a *ReasonixAdapter) Permission(ctx context.Context, action HostAction) (bool, string) {
	if a.permCheck == nil {
		return true, ""
	}
	return a.permCheck(ctx, action.Verb, action.Args)
}

// Emit forwards a navigator event into Reasonix's event sink so the frontend
// (terminal TUI, webview) can render navigator diagnostics as distinct cards.
func (a *ReasonixAdapter) Emit(ctx context.Context, ev HostEvent) {
	if a.sink == nil {
		return
	}
	level := event.LevelInfo
	if ev.Level == "warn" {
		level = event.LevelWarn
	}
	kind := event.Notice
	if ev.Kind == "deviation" || ev.Kind == "correction" {
		// Use Notice kind so deviations and corrections surface as out-of-band
		// cards the user can see without polluting the model's prompt.
		kind = event.Notice
	}
	a.sink.Emit(event.Event{
		Kind:   kind,
		Level:  level,
		Text:   ev.Text,
		Detail: fmt.Sprintf("[navigator step %d] %s — %s", ev.Step, ev.Detail, ev.Kind),
	})
}

// InterfaceProbe captures the current UI state. Delegates to the injected
// probe callback; returns "" when no probe is wired.
func (a *ReasonixAdapter) InterfaceProbe(ctx context.Context) (string, error) {
	if a.ifaceProbe == nil {
		return "", nil
	}
	return a.ifaceProbe(ctx)
}

// SnapshotEnv captures the current environment digest. Delegates to the
// injected env probe; returns "" when no probe is wired (the navigator then
// relies on its FilesystemSensor).
func (a *ReasonixAdapter) SnapshotEnv(ctx context.Context) (string, error) {
	if a.envProbe == nil {
		return "", nil
	}
	return a.envProbe(ctx)
}

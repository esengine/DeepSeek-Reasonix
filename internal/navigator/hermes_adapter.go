package navigator

import (
	"context"
	"fmt"
	"strings"
)

// HermesAdapter bridges the Navigator Kernel to the HERMES operator shell.
// HERMES will optimize its kernel on top of this one, so the adapter is the
// contract HERMES implements to embed the navigator.
//
// HERMES differences from Reasonix (Phase 1 findings):
//   - No native stop/block hook (Claude Code has one; HERMES doesn't).
//   - No pre-tool/post-tool lifecycle callbacks — hooks are simulated here.
//   - Project plugins must be manually enabled (HERMES_ENABLE_PROJECT_PLUGINS=1).
//   - Tool names differ: read_file, terminal, patch, write_file, etc.
//   - Context is injected via HERMES.md, not a system prompt slot.
//
// This adapter provides:
//  1. Tool-name mapping (navigator verbs → HERMES tool names).
//  2. Hook simulation (Pre/PostToolUse via the adapter's before/after callbacks).
//  3. A clean seam for HERMES to plug in its real execution backend.
//
// The adapter is a skeleton: Execute dispatches through an injectable backend
// function so HERMES can wire its real operator without the navigator package
// importing HERMES types. When no backend is wired, Execute returns an error
// (fail-closed — never pretend to execute).
type HermesAdapter struct {
	// backend is HERMES's real execution function. It receives the mapped
	// HERMES tool name + raw args and returns the result text + error.
	// When nil, Execute fails closed.
	backend func(ctx context.Context, hermesTool, args string) (output string, err error)

	// permBackend is HERMES's permission check. When nil, Permission returns
	// true (advisory) — HERMES's own gate is authoritative.
	permBackend func(ctx context.Context, hermesTool, args string) (bool, string)

	// ifaceProbe captures HERMES's UI state (e.g. a DOM digest or terminal
	// screen hash). When nil, no interface drift detection.
	ifaceProbe func(ctx context.Context) (string, error)

	// envProbe captures HERMES's environment digest. When nil, relies on the
	// navigator's FilesystemSensor.
	envProbe func(ctx context.Context) (string, error)

	// hooks simulates Pre/PostToolUse for HERMES, which has no native hooks.
	// The navigator calls BeforeTool before dispatch and AfterTool after;
	// HERMES plugins can use these to enforce policy or collect evidence.
	hooks HermesHookSimulator
}

// HermesHookSimulator lets HERMES simulate the Pre/PostToolUse lifecycle that
// Reasonix has natively. HERMES has no hook system, so the adapter calls these
// around each Execute. A HERMES plugin registers its hooks here.
type HermesHookSimulator struct {
	// PreTool is called before dispatch. Returning false blocks the action
	// (simulating a deny from a PreToolUse hook).
	PreTool func(ctx context.Context, hermesTool, args string) (allow bool, reason string)
	// PostTool is called after dispatch with the result. Used for evidence
	// collection and mutation tracking.
	PostTool func(ctx context.Context, hermesTool, args, output string, err error)
}

// HermesAdapterOptions holds the injectable HERMES backend functions.
type HermesAdapterOptions struct {
	Backend        func(ctx context.Context, hermesTool, args string) (string, error)
	PermBackend    func(ctx context.Context, hermesTool, args string) (bool, string)
	InterfaceProbe func(ctx context.Context) (string, error)
	EnvProbe       func(ctx context.Context) (string, error)
	Hooks          HermesHookSimulator
}

// NewHermesAdapter creates the HERMES adapter. All fields are optional except
// Backend (which must be wired before Execute is called).
func NewHermesAdapter(opts HermesAdapterOptions) *HermesAdapter {
	return &HermesAdapter{
		backend:     opts.Backend,
		permBackend: opts.PermBackend,
		ifaceProbe:  opts.InterfaceProbe,
		envProbe:    opts.EnvProbe,
		hooks:       opts.Hooks,
	}
}

// hermesToolMap maps navigator verbs to HERMES tool names. The navigator uses
// generic verbs (read/write/exec/click/type); HERMES uses operator commands.
var hermesToolMap = map[string]string{
	"read":   "read_file",
	"write":  "write_file",
	"edit":   "patch",
	"exec":   "terminal",
	"bash":   "terminal",
	"shell":  "terminal",
	"search": "grep",
	"glob":   "find",
	"click":  "mouse_click",
	"type":   "keyboard_type",
	"scroll": "mouse_scroll",
	"wait":   "sleep",
	"ask":    "ask_user",
}

// mapVerbToHermes returns the HERMES tool name for a navigator verb. Unknown
// verbs pass through unchanged so HERMES-specific tools aren't blocked.
func mapVerbToHermes(verb string) string {
	verb = strings.ToLower(strings.TrimSpace(verb))
	if mapped, ok := hermesToolMap[verb]; ok {
		return mapped
	}
	return verb
}

// Execute dispatches a HostAction through HERMES's backend, with hook
// simulation. Fails closed when no backend is wired.
func (a *HermesAdapter) Execute(ctx context.Context, action HostAction) (HostResult, error) {
	if a.backend == nil {
		return HostResult{}, fmt.Errorf("navigator: hermes adapter has no backend wired (fail-closed)")
	}
	hermesTool := mapVerbToHermes(action.Verb)

	// Simulated PreToolUse hook (HERMES has no native one).
	if a.hooks.PreTool != nil {
		if allow, reason := a.hooks.PreTool(ctx, hermesTool, action.Args); !allow {
			return HostResult{Err: fmt.Errorf("blocked by hermes pre-tool hook: %s", reason)},
				fmt.Errorf("hermes pre-tool hook blocked: %s", reason)
		}
	}

	output, err := a.backend(ctx, hermesTool, action.Args)
	result := HostResult{Output: output, Err: err}

	// Simulated PostToolUse hook (HERMES has no native one).
	if a.hooks.PostTool != nil {
		a.hooks.PostTool(ctx, hermesTool, action.Args, output, err)
	}

	return result, err
}

// Permission checks whether HERMES would allow the action. When no permBackend
// is wired, returns true (advisory).
func (a *HermesAdapter) Permission(ctx context.Context, action HostAction) (bool, string) {
	if a.permBackend == nil {
		return true, ""
	}
	return a.permBackend(ctx, mapVerbToHermes(action.Verb), action.Args)
}

// Emit forwards a navigator event to HERMES. Since HERMES has no typed event
// sink like Reasonix, events are written to the HERMES log channel via the
// injected logger. When no logger is wired, events are dropped (the navigator
// still records them in its correction history).
func (a *HermesAdapter) Emit(ctx context.Context, ev HostEvent) {
	// HERMES events go through its plugin log channel. The adapter doesn't
	// import HERMES types, so the host wires a logger via HermesAdapterOptions
	// in a future binding. For now, events are recorded in the navigator's
	// correction history (accessible via Navigator.Corrections()).
	_ = ev
}

// InterfaceProbe captures HERMES's UI state.
func (a *HermesAdapter) InterfaceProbe(ctx context.Context) (string, error) {
	if a.ifaceProbe == nil {
		return "", nil
	}
	return a.ifaceProbe(ctx)
}

// SnapshotEnv captures HERMES's environment digest.
func (a *HermesAdapter) SnapshotEnv(ctx context.Context) (string, error) {
	if a.envProbe == nil {
		return "", nil
	}
	return a.envProbe(ctx)
}

// HermesToolMapping returns the navigator→HERMES tool-name mapping table.
// Exposed so HERMES can audit and extend the mapping without editing the
// navigator package.
func HermesToolMapping() map[string]string {
	out := make(map[string]string, len(hermesToolMap))
	for k, v := range hermesToolMap {
		out[k] = v
	}
	return out
}

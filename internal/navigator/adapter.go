package navigator

import (
	"context"
	"errors"
)

// HostAction is the abstract action the navigator asks the host to execute.
// It is deliberately string-based so any host (Reasonix tool call, HERMES
// operator command) can represent its actions without importing navigator
// types. The host's adapter is responsible for parsing and dispatching.
type HostAction struct {
	// Verb is the action category: "read", "write", "exec", "click", "type",
	// "scroll", "wait", or a host-specific verb.
	Verb string
	// Target is the action's subject: a file path, a UI element id, a command.
	Target string
	// Args is the opaque argument payload (file contents, keystrokes, etc.).
	Args string
	// ReinjectFacts are implicit facts the engine determined were lost and
	// must be re-injected into the host's context before executing this action.
	ReinjectFacts []Fact
}

// HostResult is what the host reports back after executing an action.
type HostResult struct {
	// Output is the textual result the host would normally feed to the model.
	Output string
	// Err is non-nil if the action failed.
	Err error
	// InterfaceProbe is an optional UI-state string the host can supply (a
	// screenshot hash, a DOM digest). Empty if the host has no interface.
	InterfaceProbe string
}

// HostAdapter bridges the navigator kernel to whatever runtime embeds it.
// The kernel never calls a Reasonix or HERMES API directly — it goes through
// this interface, so the same kernel serves both hosts.
//
// ReasonixAdapter implements this by dispatching to the agent's tool registry
// and event sink. HermesAdapter implements it by mapping to HERMES operator
// commands and simulating the hooks HERMES lacks natively.
type HostAdapter interface {
	// Execute dispatches an action through the host's real execution path,
	// including its permission gate, hooks, and evidence recording. This is
	// the single chokepoint — the navigator never bypasses it.
	Execute(ctx context.Context, action HostAction) (HostResult, error)

	// Permission asks the host whether an action would be allowed, without
	// executing it. Used by the engine's pre-action verifier.
	Permission(ctx context.Context, action HostAction) (allowed bool, reason string)

	// Emit forwards a navigator event into the host's event stream so the host
	// UI (terminal, webview) can render navigator diagnostics.
	Emit(ctx context.Context, event HostEvent)

	// InterfaceProbe captures the current UI state as a string for hashing.
	// Returns "" if the host has no interface sensor backing.
	InterfaceProbe(ctx context.Context) (string, error)

	// SnapshotEnv captures the current environment state as a digest string.
	// Used to seed the initial state and to cross-check the sensor readings.
	SnapshotEnv(ctx context.Context) (string, error)
}

// HostEvent is a navigator-originated event the host should surface to its UI.
// It carries enough structure for a frontend to render a distinct "navigator"
// card without the navigator depending on the host's event types.
type HostEvent struct {
	Kind   string // "deviation" | "correction" | "sensor" | "state"
	Level  string // "info" | "warn" | "error"
	Text   string
	Detail string
	Step   int
}

// ErrAskHost signals that the engine could not resolve a deviation autonomously
// and is handing control back to the host. The host should surface the reason
// to the user or planner.
var ErrAskHost = errors.New("navigator: closed-loop engine requests host intervention")

// ErrRollback signals that the engine rewound to a prior state. The host
// should align its own state with the rewind point.
var ErrRollback = errors.New("navigator: closed-loop engine rolled back to prior state")

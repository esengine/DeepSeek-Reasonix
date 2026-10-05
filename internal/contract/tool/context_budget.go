package tool

import "context"

// ContextBudget is what the model is told about its remaining room. The figure
// that drives behavior is the distance to the compaction trigger, not to the
// raw window: the fold is the event the model can still act ahead of, and it
// fires well before the window is physically full.
type ContextBudget struct {
	Status          string `json:"status"` // ok|unmeasured
	TokensRemaining int    `json:"tokens_remaining,omitempty"`
	TokensUsed      int    `json:"tokens_used,omitempty"`
	CompactAt       int    `json:"compact_at,omitempty"`
	Window          int    `json:"window,omitempty"`
	Reason          string `json:"reason,omitempty"`
}

// Known reports whether the host could measure the budget at all. A provider
// declaring no context window leaves every figure zero, and a reported zero
// remaining would read as "out of room" rather than "never measured".
func (b ContextBudget) Known() bool { return b.Window > 0 && b.CompactAt > 0 }

// ContextBudgetReporter is supplied by the Agent owning the active session, on
// the call context for the same reason ContextCompressor is: parent and child
// agents share one tool schema without sharing session state.
type ContextBudgetReporter interface {
	ContextBudget() ContextBudget
}

type contextBudgetReporterKey struct{}

// WithContextBudgetReporter binds the active session's reporter to a tool call.
func WithContextBudgetReporter(ctx context.Context, reporter ContextBudgetReporter) context.Context {
	if reporter == nil {
		return ctx
	}
	return context.WithValue(ctx, contextBudgetReporterKey{}, reporter)
}

// ContextBudgetReporterFromContext returns the reporter bound to this tool call.
func ContextBudgetReporterFromContext(ctx context.Context) (ContextBudgetReporter, bool) {
	reporter, ok := ctx.Value(contextBudgetReporterKey{}).(ContextBudgetReporter)
	return reporter, ok && reporter != nil
}

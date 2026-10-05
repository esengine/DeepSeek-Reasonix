package agent

import (
	"errors"

	"reasonix/internal/contract/provider"
)

// The methods here are what a coordinator drives on the executor it wraps:
// identity for its events, the tool surface it summarises for the planner,
// and the handoff role only a coordinator may assign.

// ModelRef is the model reference the agent reports usage under.
func (a *Agent) ModelRef() string { return a.modelRef }

// ProviderName names the provider serving the agent's turns.
func (a *Agent) ProviderName() string { return a.svc.prov.Name() }

// ToolSchemas is the schema set attached to the agent's requests.
func (a *Agent) ToolSchemas() []provider.ToolSchema {
	if a == nil || a.svc.tools == nil {
		return nil
	}
	return a.svc.tools.Schemas()
}

// MarkExecutorHandoff makes the agent the executor of a planner handoff, which
// changes how it reads the handoff marker in its input.
func (a *Agent) MarkExecutorHandoff() { a.role.executorHandoff = true }

// WithTurnPreferences folds the turn's standing preferences into user input,
// as the agent does for the input it runs itself.
func (a *Agent) WithTurnPreferences(input string) string { return a.withTurnPreferences(input) }

// MaxStepsPauseOf reports the step ceiling and its key when err is a pause at
// the run's step limit.
func MaxStepsPauseOf(err error) (steps int, key string, ok bool) {
	var p *maxStepsPause
	if !errors.As(err, &p) {
		return 0, "", false
	}
	return p.steps, p.key, true
}

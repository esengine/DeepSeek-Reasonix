package builtin

import (
	"context"
	"encoding/json"
	"fmt"

	"reasonix/internal/contract/tool"
)

func init() { tool.RegisterBuiltin(contextBudget{}) }

// contextBudget answers "can I afford this?" before the model commits to work
// whose output it cannot finish reading. The figure is pull-only: the host never
// appends it to a turn or raises a notice about it.
type contextBudget struct{}

func (contextBudget) Name() string { return "context_budget" }

func (contextBudget) Description() string {
	return "Report how much context room is left before this conversation is automatically compacted. Nothing reports it unprompted: check it at the start of a long task, before a large change, and before output you may not finish reading (a broad search, a large file, a long build log), so you can narrow the command. `tokens_remaining` counts down to the compaction trigger, not the physical window."
}

func (contextBudget) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{}}`)
}

func (contextBudget) ReadOnly() bool { return true }

func (contextBudget) PlanModeSafe() bool { return true }

func (contextBudget) Execute(ctx context.Context, _ json.RawMessage) (string, error) {
	reporter, ok := tool.ContextBudgetReporterFromContext(ctx)
	if !ok {
		return "", fmt.Errorf("context_budget is unavailable outside an active agent session")
	}
	budget := reporter.ContextBudget()
	out, err := json.Marshal(budget)
	if err != nil {
		return "", fmt.Errorf("encode context budget: %w", err)
	}
	return string(out), nil
}

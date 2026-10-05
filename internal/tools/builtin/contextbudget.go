package builtin

import (
	"context"
	"encoding/json"
	"fmt"

	"reasonix/internal/contract/tool"
)

func init() { tool.RegisterBuiltin(contextBudget{}) }

// contextBudget answers "can I afford this?" before the model commits to work
// whose output it cannot finish reading. The host volunteers the same figure at
// two thresholds; this tool is the pull side, for the decision that arrives
// between them.
type contextBudget struct{}

func (contextBudget) Name() string { return "context_budget" }

func (contextBudget) Description() string {
	return "Report how much context room is left before this conversation is automatically compacted. Call it before committing to work whose output you may not be able to finish reading — a broad search, a large file, a long build log — so you can narrow the command instead of spending the rest of the window discovering it was too big. `tokens_remaining` counts down to the compaction trigger, not to the physical window, because the fold is what you can still act ahead of."
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

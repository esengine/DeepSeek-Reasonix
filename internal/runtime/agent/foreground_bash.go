package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"reasonix/internal/contract/tool"
)

func (b foregroundOnlyBash) ExecutionDescriptor(args json.RawMessage) *tool.ShellExecution {
	if detailed, ok := b.inner.(tool.DetailedExecutor); ok {
		return detailed.ExecutionDescriptor(args)
	}
	return nil
}

func (b foregroundOnlyBash) ExecuteDetailed(ctx context.Context, args json.RawMessage) (tool.DetailedResult, error) {
	var p struct {
		RunInBackground bool `json:"run_in_background"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return tool.DetailedResult{}, fmt.Errorf("invalid args: %w", err)
	}
	if p.RunInBackground {
		return tool.DetailedResult{}, tool.Blocked("blocked: background bash is unavailable in subagents; run a foreground command or ask the parent agent to start a background job")
	}
	if detailed, ok := b.inner.(tool.DetailedExecutor); ok {
		return detailed.ExecuteDetailed(ctx, args)
	}
	out, err := b.inner.Execute(ctx, args)
	return tool.DetailedResult{Output: out}, err
}

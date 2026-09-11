package control

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/mcpinteraction"
	"reasonix/internal/provider"
)

// MCPAppCallTool executes one App-initiated tools/call. Security model:
// plugin.AppInstanceTool enforces same-server, app visibility, and catalog
// generation; the controller then applies the session permission policy and
// hooks around the dispatch, records nested ToolDispatch/ToolResult events
// under the instance's parent id, and stores a LocalOnly transcript message —
// visible in the UI, never added to model context.
func (c *Controller) MCPAppCallTool(instanceToken, toolName string, args json.RawMessage) (string, error) {
	if c == nil {
		return "", fmt.Errorf("no controller")
	}
	host := c.mcp.hostRef()
	if host == nil {
		return "", fmt.Errorf("no MCP runtime")
	}
	ref, ok := host.AppInstanceTool(instanceToken, toolName)
	if !ok {
		return "", fmt.Errorf("app tool call refused: unknown instance, cross-server target, non-app tool, or stale catalog")
	}
	inst, _ := host.LookupAppInstance(instanceToken)
	callCtx, ok := host.AppInstanceContext(instanceToken)
	if !ok || callCtx.Err() != nil {
		return "", fmt.Errorf("app tool call refused: instance is closed")
	}
	target := ref.UITool()
	callID := fmt.Sprintf("mcp-app-%d", time.Now().UnixNano())
	parentID := ""
	if inst != nil {
		parentID = inst.CallID
	}

	argsJSON := args
	if len(argsJSON) == 0 {
		argsJSON = json.RawMessage(`{}`)
	}
	toolEvent := event.Tool{
		ID: callID, ParentID: parentID,
		Name:     target.Name(),
		Args:     string(argsJSON),
		ReadOnly: target.ReadOnly(),
	}
	if err := event.EmitChecked(c.sink, event.Event{Kind: event.ToolDispatch, Tool: toolEvent}); err != nil {
		return "", fmt.Errorf("persist app dispatch: %w", err)
	}

	ctx := agent.WithToolCallContext(callCtx, callID, c.sink, c, false)
	ctx = mcpinteraction.WithBroker(ctx, c)

	if c.hooks != nil {
		c.hooks.PreToolUse(ctx, target.Name(), argsJSON)
	}
	rawResult, output, reportedError, err := target.ExecuteForApp(ctx, argsJSON)
	hookErr := err
	if hookErr == nil && reportedError {
		hookErr = errors.New("MCP tool returned isError")
	}
	if c.hooks != nil {
		if hookErr != nil {
			c.hooks.PostToolUseFailure(ctx, target.Name(), argsJSON, output, hookErr)
		} else {
			c.hooks.PostToolUse(ctx, target.Name(), argsJSON, output)
		}
	}

	toolEvent.Output = output
	if hookErr != nil {
		toolEvent.Err = hookErr.Error()
	}
	if emitErr := event.EmitChecked(c.sink, event.Event{Kind: event.ToolResult, Tool: toolEvent}); emitErr != nil {
		return string(rawResult), fmt.Errorf("persist app result: %w", emitErr)
	}
	if c.executor != nil && c.executor.Session() != nil {
		c.executor.Session().Add(provider.Message{
			Role: provider.RoleTool, LocalOnly: true,
			ToolCallID: callID, Name: target.Name(),
			Content: output,
		})
	}
	return string(rawResult), err
}

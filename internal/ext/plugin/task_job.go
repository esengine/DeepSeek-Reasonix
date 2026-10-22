package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"reasonix/internal/contract/tool"
)

// taskJobCeiling bounds a promoted task whose server gave no ttlMs: a job lives
// for the session, but a poll loop nobody can see must still end.
const taskJobCeiling = 24 * time.Hour

// promoteTask moves a task that outran its inline budget into a background
// job and answers the call with that job's host-minted id. The model never
// sees the task's own handle. It reports false when the turn has no job
// manager, and the caller then ends the task instead.
func (c *Client) promoteTask(ctx context.Context, t transport, params any, task mcpTask) (json.RawMessage, bool) {
	start, ok := tool.BackgroundStarterFrom(ctx)
	if !ok {
		return nil, false
	}
	releaseSlot, ok := c.tasks.reserveBackground()
	if !ok {
		return nil, false
	}
	label := c.name + "." + rawToolNameFromCallParams(params)
	origin := tool.ProvenanceHeader(tool.Provenance{Kind: tool.ProvenanceMCP, Source: c.name})
	id := start("mcp_task", label, func(jobCtx context.Context, out io.Writer) (string, error) {
		defer releaseSlot()
		runCtx, stop := context.WithTimeout(jobCtx, taskJobCeiling)
		defer stop()
		fmt.Fprintf(out, "following the task on server %s\n", c.name)
		res, err := c.followTask(runCtx, t, &task)
		if err != nil {
			if runCtx.Err() != nil {
				c.cancelTask(t, task.TaskID)
			}
			return "", err
		}
		text, _, err := captureToolResult(runCtx, res)
		return origin + text, err
	})
	msg := fmt.Sprintf("MCP task %s is still running on the server and continues as background job %s: use wait or bash_output to collect its result, kill_shell to cancel it.", label, id)
	raw, _ := json.Marshal(map[string]any{"content": []map[string]any{{"type": "text", "text": msg}}})
	return raw, true
}

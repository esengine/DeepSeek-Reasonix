package plugin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"time"

	"reasonix/internal/contract/tool"
)

var ErrMCPTaskRecoveryBinding = errors.New("MCP task recovery identity or authorization changed")

func remoteTaskBinding(s Spec) string {
	identity := struct {
		Schema, Workspace, Source, Package string
		Env, Headers                       map[string]string
	}{SchemaCacheKey(s), s.WorkspaceRoot, s.ConfigSource, s.Package, s.Env, s.Headers}
	b, _ := json.Marshal(identity)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func (c *Client) awaitDurableTask(ctx context.Context, t transport, params any, task mcpTask, start tool.RemoteTaskStarter) (json.RawMessage, error) {
	desc := tool.RemoteTask{Server: c.name, Tool: rawToolNameFromCallParams(params), TaskID: task.TaskID,
		Binding: remoteTaskBinding(c.spec), Protocol: c.modern.version, Deadline: time.Now().Add(taskJobCeiling).UnixMilli()}
	handle, err := start(desc, func(runCtx context.Context, out io.Writer) (tool.RemoteTaskResult, error) {
		if elicitor, ok := tool.ElicitorFrom(ctx); ok {
			runCtx = tool.WithElicitor(runCtx, elicitor)
		}
		return c.runDurableTask(runCtx, t, desc, task, out)
	})
	if err != nil {
		return nil, fmt.Errorf("accepted MCP task could not be checkpointed; do not repeat its submission: %w", err)
	}
	waitCtx, stop, _ := c.contextWithCallTimeout(ctx, "tools/call", params)
	if stop != nil {
		defer stop()
	}
	res, err := handle.Wait(waitCtx)
	if ctx.Err() != nil {
		if !tool.RemoteTaskDetached(ctx) {
			handle.Cancel()
		}
		return nil, ctx.Err()
	}
	if waitCtx.Err() == context.DeadlineExceeded {
		if handle.Promote != nil {
			handle.Promote()
		}
		msg := fmt.Sprintf("MCP task %s.%s continues as background job %s: use wait or bash_output to collect its result, kill_shell to cancel it.", c.name, desc.Tool, handle.ID)
		raw, _ := json.Marshal(map[string]any{"content": []map[string]any{{"type": "text", "text": msg}}})
		return raw, nil
	}
	if len(res) > 0 {
		return res, nil
	}
	return res, err
}

func (c *Client) runDurableTask(ctx context.Context, t transport, desc tool.RemoteTask, task mcpTask, out io.Writer) (tool.RemoteTaskResult, error) {
	runCtx, stop := context.WithDeadline(ctx, time.UnixMilli(desc.Deadline))
	defer stop()
	defer func() {
		if runCtx.Err() != nil && !tool.RemoteTaskDetached(ctx) {
			c.cancelTask(t, desc.TaskID)
		}
	}()
	release, err := c.tasks.acquire(runCtx)
	if err != nil {
		return tool.RemoteTaskResult{}, err
	}
	defer release()
	fmt.Fprintf(out, "following the task on server %s\n", c.name)
	raw, err := c.followTask(runCtx, t, &task)
	terminal := len(raw) > 0 || errors.Is(err, ErrMCPTaskFailed) || errors.Is(err, ErrMCPTaskCancelled) || errors.Is(err, ErrMCPTaskGone)
	result := tool.RemoteTaskResult{Result: raw, Terminal: terminal}
	if len(raw) > 0 {
		text, _, parseErr := parseToolResult(raw)
		var envelope struct {
			Structured json.RawMessage `json:"structuredContent"`
		}
		if json.Unmarshal(raw, &envelope) == nil {
			if fields := structuredFields(envelope.Structured); fields != "" {
				text += "\n[structuredContent fields: " + fields + "]"
			}
		}
		result.Output = tool.ProvenanceHeader(tool.Provenance{Kind: tool.ProvenanceMCP, Source: c.name}) + text
		err = errors.Join(err, parseErr)
	}
	return result, err
}

// RemoteTaskResumer queries an existing task through its currently authorized server.
func (h *Host) RemoteTaskResumer(specs []Spec) tool.RemoteTaskResumer {
	specs = slices.Clone(specs)
	return func(ctx context.Context, desc tool.RemoteTask, out io.Writer) (tool.RemoteTaskResult, error) {
		var spec *Spec
		for _, candidate := range specs {
			if candidate.Name == desc.Server {
				resolved := ResolveStoredAuthorization(ctx, candidate)
				if resolved.ServerAuthorized() && resolved.ToolEnabled(desc.Tool) && remoteTaskBinding(resolved) == desc.Binding {
					spec = &resolved
					break
				}
			}
		}
		if spec == nil || desc.Protocol != modernProtocolVersion {
			return tool.RemoteTaskResult{}, ErrMCPTaskRecoveryBinding
		}
		if err := checkDeclaredLaunch(*spec); err != nil {
			return tool.RemoteTaskResult{}, err
		}
		if _, err := h.EnsureConnectedWithLifecycle(ctx, ctx, *spec, 0); err != nil {
			return tool.RemoteTaskResult{}, err
		}
		c := h.lookupClient(desc.Server)
		if c == nil || c.modern.version != desc.Protocol || remoteTaskBinding(c.spec) != desc.Binding {
			return tool.RemoteTaskResult{}, ErrMCPTaskRecoveryBinding
		}
		if desc.CancelRequested {
			if _, err := c.taskCall(ctx, c.t, "tasks/cancel", desc.TaskID, nil); err != nil {
				return tool.RemoteTaskResult{}, err
			}
		}
		raw, err := c.taskCall(ctx, c.t, "tasks/get", desc.TaskID, nil)
		if err != nil {
			var rpc *rpcError
			if errors.As(err, &rpc) && rpc.Code == -32602 {
				return tool.RemoteTaskResult{Terminal: true}, ErrMCPTaskGone
			}
			return tool.RemoteTaskResult{}, err
		}
		task, err := decodeTask(raw)
		if err != nil {
			return tool.RemoteTaskResult{}, err
		}
		task.TaskID = desc.TaskID
		return c.runDurableTask(ctx, c.t, desc, task, out)
	}
}

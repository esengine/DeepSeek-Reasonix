package delegation

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"

	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/runtime/writeclaim"
)

const maxParentMessageBytes = 16 * 1024
const maxParentMessages = 32

func (t *TaskTool) collaboratingChildRegistry(ctx context.Context, spec *ProfileExecSpec, depth int, grant *writeclaim.WriteGrant, model string) (*tool.Registry, error) {
	reg, err := t.childRegistry(spec, depth, grant, model)
	if err != nil {
		return nil, err
	}
	attachChildCommunication(ctx, reg, *spec)
	return reg, nil
}

func attachChildCommunication(ctx context.Context, reg *tool.Registry, spec ProfileExecSpec) {
	attachTaskContext(reg, spec.Context.Upstream)
	receive := agent.ChildMessageReceiverFrom(ctx)
	if !spec.Context.Ephemeral && receive != nil {
		reg.Add(&parentMessageTool{sender: agent.ChildMessage{Name: spec.Worker.Name, RunID: rand.Text()}, receive: receive})
	}
}

func bindChildCommunication(reg *tool.Registry, run *SubagentRun, execution string, upstream []UpstreamResult) {
	if candidate, ok := reg.Get("reply_to_parent"); ok {
		if reply, ok := candidate.(*parentMessageTool); ok {
			reply.sender.Ref = firstNonEmpty(run.Ref, execution)
		}
	}
	run.Meta.Capsule.Inherited.ResultRefs = upstreamRefs(upstream)
	run.Meta.CapsuleHash = run.Meta.Capsule.Hash()
}

type parentMessageTool struct {
	sender  agent.ChildMessage
	receive agent.ChildMessageReceiver
	mu      sync.Mutex
	sent    int
}

func init() { tool.RegisterConditional(&parentMessageTool{}) }

func (*parentMessageTool) Name() string { return "reply_to_parent" }
func (*parentMessageTool) Description() string {
	return "Send a concise finding, question, or progress report to the agent that delegated this task, then continue working. The runtime binds the recipient and your identity. Success means queued, not read or approved. This is not the final task result and cannot grant permissions. At most 32 messages per run, each at most 16384 UTF-8 bytes."
}
func (*parentMessageTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"message":{"type":"string","description":"The report for the delegating agent."}},"required":["message"],"additionalProperties":false}`)
}

// Like complete_subtask, this reports within the delegation without workspace authority.
func (*parentMessageTool) ReadOnly() bool     { return true }
func (*parentMessageTool) PlanModeSafe() bool { return true }
func (*parentMessageTool) Reach() tool.Reach  { return tool.ReachHostControl }

func (t *parentMessageTool) Execute(ctx context.Context, raw json.RawMessage) (string, error) {
	var args struct {
		Message string `json:"message"`
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&args); err != nil {
		return "", err
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return "", fmt.Errorf("expected one message object")
	}
	message := strings.TrimSpace(args.Message)
	if message == "" || len(message) > maxParentMessageBytes {
		return "", fmt.Errorf("message must contain 1 to %d UTF-8 bytes", maxParentMessageBytes)
	}
	if t.receive == nil {
		return "", fmt.Errorf("this run has no parent message receiver")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	t.mu.Lock()
	if t.sent >= maxParentMessages {
		t.mu.Unlock()
		return "", fmt.Errorf("parent message limit reached; include further findings in the final result")
	}
	t.sent++
	sequence := t.sent
	t.mu.Unlock()
	m := t.sender
	m.Text = message
	m.CallID, _, _, _ = agent.CallContext(ctx)
	// Providers may reuse call IDs in later rounds of the same run. Delivery
	// retries of this invocation keep its key; separate reports must not collide.
	m.CallID = fmt.Sprintf("%s:%d", m.CallID, sequence)
	if err := t.receive(ctx, m); err != nil {
		return "", err
	}
	return "Report queued for the delegating agent; it has not necessarily been read. Continue this task and return its final result when finished.", nil
}

package agent

import (
	"context"
	"encoding/json"
)

type probeInertTool struct{}

func (probeInertTool) Name() string { return "inert" }

func (probeInertTool) Description() string { return "does not delegate" }

func (probeInertTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }

func (probeInertTool) ReadOnly() bool { return true }

func (probeInertTool) Execute(context.Context, json.RawMessage) (string, error) {
	return "", nil
}

type subagentRegistryTool struct {
	name     string
	schema   string
	readOnly bool
	result   string
}

func (t subagentRegistryTool) Name() string { return t.name }

func (t subagentRegistryTool) Description() string {
	return "Execute a command in the shell and return combined stdout/stderr."
}

func (t subagentRegistryTool) Schema() json.RawMessage {
	if t.schema != "" {
		return json.RawMessage(t.schema)
	}
	return json.RawMessage(`{"type":"object"}`)
}

func (t subagentRegistryTool) ReadOnly() bool { return t.readOnly }

func (t subagentRegistryTool) Execute(context.Context, json.RawMessage) (string, error) {
	return t.result, nil
}

func testTaskContext() context.Context {
	return WithParentSession(context.Background(), "parent-session")
}

package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"reasonix/internal/agent"
	"reasonix/internal/tool"
)

type agentSpawnTool struct {
	orc *Orchestrator
}

func (t *agentSpawnTool) Name() string        { return "agent_spawn" }
func (t *agentSpawnTool) Description() string  { return "Delegate a complete task to a named managed agent. The agent runs independently with its own model, tools, and context. When you receive the result, the agent has finished the work — integrate the outcome and move on. Do not repeat the delegated work." }
func (t *agentSpawnTool) ReadOnly() bool       { return false }

func (t *agentSpawnTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"name": {"type": "string", "description": "Name of the managed agent to delegate to"},
			"task": {"type": "string", "description": "The task or question for the agent"}
		},
		"required": ["name", "task"]
	}`)
}

func (t *agentSpawnTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Name string `json:"name"`
		Task string `json:"task"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("agent_spawn: invalid args: %w", err)
	}
	if p.Name == "" {
		return "", fmt.Errorf("agent_spawn: name is required")
	}
	if p.Task == "" {
		return "", fmt.Errorf("agent_spawn: task is required")
	}

	a, ok := t.orc.Agent(p.Name)
	if !ok {
		names := strings.Join(t.orc.AgentNames(), ", ")
		return "", fmt.Errorf("agent_spawn: agent %q not found. Available agents: %s", p.Name, names)
	}

	parentID, parent, _, ok := agent.CallContext(ctx)
	if ok && parent != nil {
		a.Sink.SetParentID(parentID)
	}

	result, err := a.Run(ctx, p.Task)
	if err != nil {
		return fmt.Sprintf("[Agent %q completed with error] %v\n\nPartial result: %s", p.Name, err, result), nil
	}
	return fmt.Sprintf("[Agent %q completed]\n\n%s", p.Name, result), nil
}

type agentSendTool struct {
	orc *Orchestrator
}

func (t *agentSendTool) Name() string        { return "agent_send" }
func (t *agentSendTool) Description() string  { return "Send a message to a managed agent and wait for its response. Unlike agent_spawn, this continues the agent's existing conversation context. When you receive the result, the agent has finished responding — integrate the outcome and move on. Do not repeat the delegated work." }
func (t *agentSendTool) ReadOnly() bool       { return false }

func (t *agentSendTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"name": {"type": "string", "description": "Name of the managed agent to message"},
			"message": {"type": "string", "description": "The message to send"}
		},
		"required": ["name", "message"]
	}`)
}

func (t *agentSendTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Name    string `json:"name"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("agent_send: invalid args: %w", err)
	}
	if p.Name == "" {
		return "", fmt.Errorf("agent_send: name is required")
	}
	if p.Message == "" {
		return "", fmt.Errorf("agent_send: message is required")
	}

	parentID, parent, _, ok := agent.CallContext(ctx)
	if ok && parent != nil {
		if a, found := t.orc.Agent(p.Name); found {
			a.Sink.SetParentID(parentID)
		}
	}

	result, err := t.orc.SendMessage(ctx, p.Name, p.Message)
	if err != nil {
		return fmt.Sprintf("[Agent %q completed with error] %v", p.Name, err), nil
	}
	return fmt.Sprintf("[Agent %q completed]\n\n%s", p.Name, result), nil
}

type agentStatusTool struct {
	orc *Orchestrator
}

func (t *agentStatusTool) Name() string        { return "agent_status" }
func (t *agentStatusTool) Description() string  { return "Get the status of a managed agent (idle/running, turn count, last task)." }
func (t *agentStatusTool) ReadOnly() bool       { return true }

func (t *agentStatusTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"name": {"type": "string", "description": "Name of the managed agent (omit to list all)"}
		}
	}`)
}

func (t *agentStatusTool) Execute(_ context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Name string `json:"name"`
	}
	_ = json.Unmarshal(args, &p)

	if p.Name != "" {
		a, ok := t.orc.Agent(p.Name)
		if !ok {
			names := strings.Join(t.orc.AgentNames(), ", ")
			return "", fmt.Errorf("agent %q not found. Available: %s", p.Name, names)
		}

		usage := a.SessionUsage()
		return fmt.Sprintf("Agent: %s\nStatus: %s\nTurns: %d\nTokens: %d total (prompt %d + completion %d)\nCache: %d hit / %d miss\nLast task: %s\nCost: $%.4f",
			a.Name, a.Status(), a.TurnCount(),
			usage.TotalTokens, usage.PromptTokens, usage.CompletionTokens,
			usage.CacheHitTokens, usage.CacheMissTokens,
			a.LastTask(), usage.Cost), nil
	}

	return t.orc.StatsAll(), nil
}

type agentStatsTool struct {
	orc *Orchestrator
}

func (t *agentStatsTool) Name() string        { return "agent_stats" }
func (t *agentStatsTool) Description() string  { return "Get detailed token/cost statistics for a managed agent." }
func (t *agentStatsTool) ReadOnly() bool       { return true }

func (t *agentStatsTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"name": {"type": "string", "description": "Name of the managed agent"}
		},
		"required": ["name"]
	}`)
}

func (t *agentStatsTool) Execute(_ context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("agent_stats: invalid args: %w", err)
	}
	if p.Name == "" {
		return t.orc.StatsAll(), nil
	}
	return t.orc.Stats(p.Name), nil
}

func OrchestratorTools(orc *Orchestrator) []tool.Tool {
	return []tool.Tool{
		&agentSpawnTool{orc: orc},
		&agentSendTool{orc: orc},
		&agentStatusTool{orc: orc},
		&agentStatsTool{orc: orc},
	}
}

func OrchestratorToolNames() []string {
	return []string{
		"agent_spawn",
		"agent_send",
		"agent_status",
		"agent_stats",
	}
}



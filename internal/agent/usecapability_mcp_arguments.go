package agent

import (
	"encoding/json"
	"fmt"
	"strings"
)

type useCapabilityArgs struct {
	Action       string          `json:"action"`
	CapabilityID string          `json:"capability_id"`
	Arguments    json.RawMessage `json:"arguments"`
	Reason       string          `json:"reason"`
}

func parseUseCapabilityArgs(raw json.RawMessage) (useCapabilityArgs, string, string, error) {
	var args useCapabilityArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return args, "", "", fmt.Errorf("invalid args: %w", err)
	}
	action := strings.ToLower(strings.TrimSpace(args.Action))
	id := strings.TrimSpace(args.CapabilityID)
	if action == "call" && strings.HasPrefix(id, "mcp-tool:") {
		normalized, err := normalizeMCPToolArguments(args.Arguments)
		if err != nil {
			return args, "", "", err
		}
		args.Arguments = normalized
	}
	return args, action, id, nil
}

// normalizeMCPToolArguments accepts an object or one JSON string containing an
// object. It runs before permission, hooks, evidence, audit, and execution.
func normalizeMCPToolArguments(raw json.RawMessage) (json.RawMessage, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return json.RawMessage(`{}`), nil
	}
	if strings.HasPrefix(trimmed, `"`) {
		var inner string
		if err := json.Unmarshal(raw, &inner); err != nil {
			return nil, fmt.Errorf("arguments for an MCP tool must be a JSON object or a single JSON string containing an object: %w", err)
		}
		trimmed = strings.TrimSpace(inner)
	}
	var object map[string]any
	if !strings.HasPrefix(trimmed, "{") || json.Unmarshal([]byte(trimmed), &object) != nil || object == nil {
		return nil, fmt.Errorf("arguments for an MCP tool must be a JSON object; arrays, scalars, malformed JSON, and nested JSON strings are not supported")
	}
	return json.RawMessage(append([]byte(nil), trimmed...)), nil
}

package agent

import (
	"encoding/json"
	"fmt"
	"strings"
)

type useCapabilityArgs struct {
	Action       string          `json:"action"`
	CapabilityID string          `json:"capability_id"`
	Query        string          `json:"query"`
	Limit        int             `json:"limit"`
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
	if args.Limit < 0 || args.Limit > 8 {
		return args, "", "", fmt.Errorf("limit must be between 1 and 8 when provided")
	}
	if action == "call" && strings.HasPrefix(id, "mcp-tool:") {
		normalized, err := normalizeMCPToolArguments(args.Arguments)
		if err != nil {
			return args, "", "", err
		}
		args.Arguments = normalized
	}
	return args, action, id, nil
}

// normalizeMCPToolArguments accepts only an object. It deliberately does not
// unwrap JSON strings, rename fields, coerce values, or guess enums: schema
// mistakes must produce one precise repair contract instead of hidden behavior
// that differs between direct and proxied MCP calls.
func normalizeMCPToolArguments(raw json.RawMessage) (json.RawMessage, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return json.RawMessage(`{}`), nil
	}
	var object map[string]any
	if !strings.HasPrefix(trimmed, "{") || json.Unmarshal([]byte(trimmed), &object) != nil || object == nil {
		return nil, fmt.Errorf("arguments for an MCP tool must be a JSON object; arrays, scalars, malformed JSON, and nested JSON strings are not supported")
	}
	return json.RawMessage(append([]byte(nil), trimmed...)), nil
}

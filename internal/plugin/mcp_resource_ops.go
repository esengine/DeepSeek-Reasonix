package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// CallResource runs one MCP resources/* method on a connected server and
// returns JSON with binary blobs replaced by length notes for the model.
func (h *Host) CallResource(ctx context.Context, server, method string, params map[string]any) (json.RawMessage, error) {
	server = strings.TrimSpace(server)
	if server == "" {
		return nil, fmt.Errorf("mcp resource: server is required")
	}
	if h == nil {
		return nil, fmt.Errorf("no MCP server named %q", server)
	}
	target := h.lookupClient(server)
	if target == nil {
		return nil, fmt.Errorf("no MCP server named %q", server)
	}
	if params == nil {
		params = map[string]any{}
	}
	raw, err := target.call(ctx, method, params)
	if err != nil {
		return nil, err
	}
	return redactResourceBlobs(raw), nil
}

func redactResourceBlobs(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return raw
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return raw
	}
	redactBlobValues(value)
	out, err := json.Marshal(value)
	if err != nil {
		return raw
	}
	return out
}

func redactBlobValues(value any) {
	switch typed := value.(type) {
	case map[string]any:
		if blob, ok := typed["blob"].(string); ok {
			typed["blob"] = fmt.Sprintf("[binary resource: %d base64 characters; omitted from model text]", len(blob))
		}
		for _, child := range typed {
			redactBlobValues(child)
		}
	case []any:
		for _, child := range typed {
			redactBlobValues(child)
		}
	}
}

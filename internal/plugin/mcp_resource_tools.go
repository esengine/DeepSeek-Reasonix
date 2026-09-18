package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"reasonix/internal/tool"
)

const (
	ListMCPResourcesName         = "list_mcp_resources"
	ListMCPResourceTemplatesName = "list_mcp_resource_templates"
	ReadMCPResourceName          = "read_mcp_resource"
)

// MCPResourceToolNames is the stable provider-visible order when MCP is configured.
func MCPResourceToolNames() []string {
	return []string{ListMCPResourcesName, ListMCPResourceTemplatesName, ReadMCPResourceName}
}

type mcpResourceTool struct {
	host     *Host
	name     string
	desc     string
	schema   json.RawMessage
	method   string
	needsURI bool
}

// NewMCPResourceTools returns the three shared on-demand resource tools.
func NewMCPResourceTools(host *Host) []tool.Tool {
	listSchema := json.RawMessage(`{"type":"object","properties":{"server":{"type":"string","description":"Configured MCP server name."},"cursor":{"type":"string","description":"Continuation cursor returned by this server."}},"required":["server"]}`)
	readSchema := json.RawMessage(`{"type":"object","properties":{"server":{"type":"string","description":"Configured MCP server name."},"uri":{"type":"string","description":"Resource URI to read."}},"required":["server","uri"]}`)
	return []tool.Tool{
		mcpResourceTool{host: host, name: ListMCPResourcesName, desc: "List resources available from an MCP server.", schema: listSchema, method: "resources/list"},
		mcpResourceTool{host: host, name: ListMCPResourceTemplatesName, desc: "List parameterized resource URI templates from an MCP server.", schema: listSchema, method: "resources/templates/list"},
		mcpResourceTool{host: host, name: ReadMCPResourceName, desc: "Read an MCP resource by URI from the named server. Use a listed URI or an expanded resource template.", schema: readSchema, method: "resources/read", needsURI: true},
	}
}

func (t mcpResourceTool) Name() string        { return t.name }
func (t mcpResourceTool) Description() string { return t.desc }
func (t mcpResourceTool) Schema() json.RawMessage {
	return t.schema
}
func (t mcpResourceTool) ReadOnly() bool { return true }

func (t mcpResourceTool) Execute(ctx context.Context, raw json.RawMessage) (string, error) {
	var args struct {
		Server string `json:"server"`
		Cursor string `json:"cursor"`
		URI    string `json:"uri"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			return "", fmt.Errorf("%s: %w", t.name, err)
		}
	}
	args.Server = strings.TrimSpace(args.Server)
	if args.Server == "" {
		return "", fmt.Errorf("%s: server is required", t.name)
	}
	params := map[string]any{}
	if t.needsURI {
		args.URI = strings.TrimSpace(args.URI)
		if args.URI == "" {
			return "", fmt.Errorf("%s: uri is required", t.name)
		}
		params["uri"] = args.URI
	} else if cursor := strings.TrimSpace(args.Cursor); cursor != "" {
		params["cursor"] = cursor
	}
	out, err := t.host.CallResource(ctx, args.Server, t.method, params)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("MCP server: %s\n%s", args.Server, string(out)), nil
}

package tool

import "strings"

// ParseMCPServerReference accepts the server references used by skill allowlists.
func ParseMCPServerReference(id string) (server string, ok bool) {
	if !strings.HasPrefix(id, "mcp-server:") {
		return "", false
	}
	server = strings.TrimSpace(strings.TrimPrefix(id, "mcp-server:"))
	return server, server != "" && !strings.Contains(server, "/")
}

// ParseMCPToolReference accepts the tool references used by skill allowlists.
func ParseMCPToolReference(id string) (server, raw string, ok bool) {
	if !strings.HasPrefix(id, "mcp-tool:") {
		return "", "", false
	}
	server, raw, cut := strings.Cut(strings.TrimPrefix(id, "mcp-tool:"), "/")
	server, raw = strings.TrimSpace(server), strings.TrimSpace(raw)
	return server, raw, cut && server != "" && raw != ""
}

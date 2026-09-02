package provider

import "encoding/json"

// MCPAppPresentation is the host-local MCP Apps payload: the standard
// CallToolResult plus the resource identity needed to restore the App UI.
// Bounded to 512 KiB with inline audio/video and oversized base64 removed
// before attach; provider serializers must never emit this object.
type MCPAppPresentation struct {
	Server      string              `json:"server"`
	Tool        string              `json:"tool"`
	Generation  uint64              `json:"generation"`
	ResourceURI string              `json:"resourceUri,omitempty"`
	CSP         map[string][]string `json:"csp,omitempty"`
	RawResult   json.RawMessage     `json:"rawResult,omitempty"`
	Structured  json.RawMessage     `json:"structured,omitempty"`
}

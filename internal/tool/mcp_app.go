package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// MCPAppResult is the host-local MCP Apps payload one tools/call can produce:
// the standard CallToolResult plus the resource identity a Desktop App needs
// to restore its UI. It travels on the call context (never the tool) and is
// converted to the provider-excluded provider.MCPAppPresentation by the agent.
type MCPAppResult struct {
	Server      string
	Tool        string
	Generation  uint64
	ResourceURI string
	CSP         map[string][]string
	RawResult   json.RawMessage
	Structured  json.RawMessage
}

// maxMCPAppBytes bounds the persisted presentation copy. Oversized results
// keep the text form only.
const maxMCPAppBytes = 512 << 10

// ValidateMCPAppCallResult preserves a complete standard CallToolResult for
// the App bridge while enforcing the same bounded host payload used by stored
// presentations. Unknown fields and nested resource metadata are retained.
func ValidateMCPAppCallResult(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 || len(raw) > maxMCPAppBytes {
		return nil, fmt.Errorf("MCP App tool result is empty or exceeds %d bytes", maxMCPAppBytes)
	}
	var result map[string]any
	if err := json.Unmarshal(raw, &result); err != nil || result == nil {
		return nil, fmt.Errorf("MCP App tool result is not a JSON object")
	}
	if _, ok := result["content"].([]any); !ok {
		return nil, fmt.Errorf("MCP App tool result has invalid content")
	}
	return append(json.RawMessage(nil), raw...), nil
}

// droppableMCPAppContent reports items that never enter the persisted copy:
// audio/video and oversized base64 data blow the budget without adding App
// value; the model-facing text/image forms live on the message itself.
func droppableMCPAppContent(item map[string]any) bool {
	if hasOversizedInlineData(item) {
		return true
	}
	switch item["type"] {
	case "audio", "video":
		return true
	}
	mime, _ := item["mimeType"].(string)
	return strings.HasPrefix(mime, "audio/") || strings.HasPrefix(mime, "video/")
}

func hasOversizedInlineData(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, nested := range typed {
			if key == "data" || key == "blob" {
				if text, ok := nested.(string); ok && len(text) > 4096 {
					return true
				}
			}
			if hasOversizedInlineData(nested) {
				return true
			}
		}
	case []any:
		return slices.ContainsFunc(typed, hasOversizedInlineData)
	}
	return false
}

func sanitizeMCPAppRawResult(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil || parsed == nil {
		return nil
	}
	if content, ok := parsed["content"].([]any); ok {
		kept := make([]any, 0, len(content))
		for _, candidate := range content {
			item, ok := candidate.(map[string]any)
			if !ok || !droppableMCPAppContent(item) {
				kept = append(kept, candidate)
			}
		}
		parsed["content"] = kept
	}
	b, err := json.Marshal(parsed)
	if err != nil {
		return nil
	}
	return b
}

func cloneMCPAppCSP(csp map[string][]string) map[string][]string {
	if len(csp) == 0 {
		return nil
	}
	out := make(map[string][]string, len(csp))
	for directive, values := range csp {
		out[directive] = append([]string(nil), values...)
	}
	return out
}

func mcpAppPersistedSize(r *MCPAppResult) int {
	if r == nil {
		return 0
	}
	wire := struct {
		Server      string              `json:"server"`
		Tool        string              `json:"tool"`
		Generation  uint64              `json:"generation"`
		ResourceURI string              `json:"resourceUri,omitempty"`
		CSP         map[string][]string `json:"csp,omitempty"`
		RawResult   json.RawMessage     `json:"rawResult,omitempty"`
		Structured  json.RawMessage     `json:"structured,omitempty"`
	}{
		Server: r.Server, Tool: r.Tool, Generation: r.Generation,
		ResourceURI: r.ResourceURI, CSP: r.CSP,
		RawResult: r.RawResult, Structured: r.Structured,
	}
	b, err := json.Marshal(wire)
	if err != nil {
		return maxMCPAppBytes + 1
	}
	return len(b)
}

// Sanitized returns the bounded presentation copy: inline audio/video and
// oversized base64 content items are dropped, then the whole payload is
// capped at maxMCPAppBytes (text fallback remains in Content).
func (r *MCPAppResult) Sanitized() *MCPAppResult {
	if r == nil || strings.TrimSpace(r.Server) == "" || strings.TrimSpace(r.Tool) == "" {
		return nil
	}
	out := &MCPAppResult{
		Server:      r.Server,
		Tool:        r.Tool,
		Generation:  r.Generation,
		ResourceURI: r.ResourceURI,
		CSP:         cloneMCPAppCSP(r.CSP),
	}
	out.RawResult = sanitizeMCPAppRawResult(r.RawResult)
	var structuredCopy json.RawMessage
	if json.Valid(r.Structured) {
		structuredCopy = append(json.RawMessage(nil), r.Structured...)
		out.Structured = structuredCopy
	}
	// Enforce one aggregate persisted budget, including identity, CSP, JSON
	// framing, RawResult, and Structured. Structured is normally duplicated in
	// RawResult, so discard it first; then the rich result; then optional CSP.
	if mcpAppPersistedSize(out) > maxMCPAppBytes {
		out.Structured = nil
	}
	if mcpAppPersistedSize(out) > maxMCPAppBytes {
		out.RawResult = nil
		out.Structured = structuredCopy
	}
	if mcpAppPersistedSize(out) > maxMCPAppBytes {
		out.Structured = nil
	}
	if mcpAppPersistedSize(out) > maxMCPAppBytes {
		out.CSP = nil
	}
	if mcpAppPersistedSize(out) > maxMCPAppBytes {
		return nil
	}
	return out
}

type mcpAppKey struct{}

// WithMCPAppCollector attaches a presentation collector the executing tool
// fills in. Contexts are immutable, so the callee cannot hand a value back by
// deriving a new ctx; the collector is a shared cell the caller reads after
// the call. Returns the collector alongside the derived context.
func WithMCPAppCollector(ctx context.Context) (context.Context, *MCPAppResult) {
	if ctx == nil {
		ctx = context.Background()
	}
	sink := &MCPAppResult{}
	return context.WithValue(ctx, mcpAppKey{}, sink), sink
}

// CollectMCPAppResult fills the call's collector; a no-op without one.
func CollectMCPAppResult(ctx context.Context, r *MCPAppResult) {
	if ctx == nil || r == nil {
		return
	}
	if sink, ok := ctx.Value(mcpAppKey{}).(*MCPAppResult); ok && sink != nil {
		*sink = *r
	}
}

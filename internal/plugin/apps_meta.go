package plugin

import (
	"context"
	"encoding/json"

	"reasonix/internal/tool"
)

type mcpTool struct {
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	InputSchema  json.RawMessage `json:"inputSchema"`
	OutputSchema json.RawMessage `json:"outputSchema,omitempty"`
	// Annotations carries MCP's optional tool hints. readOnlyHint controls reader
	// classification; destructiveHint remains destructive even when another hint
	// claims the tool is read-only. Approval policy is applied separately.
	Annotations *struct {
		ReadOnlyHint    bool `json:"readOnlyHint"`
		DestructiveHint bool `json:"destructiveHint"`
	} `json:"annotations"`
	// Meta carries MCP Apps tool metadata: visibility ("model"/"app") and the
	// ui resource the App renders from.
	Meta *mcpToolMeta `json:"_meta,omitempty"`
}

type mcpToolMeta struct {
	// Visibility is the legacy top-level shape. Stable MCP Apps nests the
	// field under ui; the nested value wins when both are present.
	Visibility []string `json:"visibility,omitempty"`
	// ResourceURI is the pre-2026-01-26 flat key; ui.resourceUri wins.
	ResourceURI string `json:"resourceUri,omitempty"`
	// FlatResourceURI is the ext-apps "ui/resourceUri" flat key.
	FlatResourceURI string `json:"ui/resourceUri,omitempty"`
	UI              *struct {
		ResourceURI string              `json:"resourceUri,omitempty"`
		CSP         map[string][]string `json:"csp,omitempty"`
		Visibility  []string            `json:"visibility,omitempty"`
	} `json:"ui,omitempty"`
}

func (m *mcpToolMeta) effectiveVisibility() []string {
	if m == nil {
		return nil
	}
	if m.UI != nil && len(m.UI.Visibility) > 0 {
		return m.UI.Visibility
	}
	return m.Visibility
}

// appVisibility resolves the effective visibility: the spec default keeps a
// tool in both the model catalog and the App channel.
func (m *mcpToolMeta) appVisibility() (model, app bool) {
	visibility := m.effectiveVisibility()
	if len(visibility) == 0 {
		return true, true
	}
	for _, v := range visibility {
		switch v {
		case "model":
			model = true
		case "app":
			app = true
		}
	}
	return model, app
}

// visibilityCopy is nil-safe for tools without _meta.
func (m *mcpToolMeta) visibilityCopy() []string {
	return append([]string(nil), m.effectiveVisibility()...)
}

func (m *mcpToolMeta) uiResource() (uri string, csp map[string][]string) {
	if m == nil {
		return "", nil
	}
	if m.UI != nil {
		if m.UI.ResourceURI != "" {
			uri = m.UI.ResourceURI
		}
		if len(m.UI.CSP) > 0 {
			csp = m.UI.CSP
		}
	}
	if uri == "" {
		uri = m.ResourceURI
	}
	if uri == "" {
		uri = m.FlatResourceURI
	}
	return uri, csp
}

// stampMCPAppResult attaches the bounded MCP Apps presentation to the call
// context when the result carries App-relevant payload (a structured result
// or a tool with a ui resource). It never alters the text/image forms.
func stampMCPAppResult(ctx context.Context, t *remoteTool, res json.RawMessage) {
	if t == nil || t.client == nil || !t.client.appsNegotiated() {
		return
	}
	var wire struct {
		StructuredContent json.RawMessage `json:"structuredContent"`
	}
	_ = json.Unmarshal(res, &wire)
	if t.uiResourceURI == "" && len(wire.StructuredContent) == 0 {
		return
	}
	tool.CollectMCPAppResult(ctx, (&tool.MCPAppResult{
		Server:      t.client.name,
		Tool:        t.rawName,
		Generation:  t.generation,
		ResourceURI: t.uiResourceURI,
		CSP:         t.uiCSP,
		RawResult:   append(json.RawMessage(nil), res...),
		Structured:  append(json.RawMessage(nil), wire.StructuredContent...),
	}).Sanitized())
}

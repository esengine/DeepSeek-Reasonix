package plugin

import (
	"context"
	"encoding/json"
	"log/slog"
)

func (c *Client) initialize(ctx context.Context) error {
	res, err := c.call(ctx, "initialize", nil)
	if err != nil {
		return err
	}
	// Record which optional capabilities the server advertises. Presence of the
	// key (even with an empty object) signals support.
	var ir struct {
		ProtocolVersion string                     `json:"protocolVersion"`
		Capabilities    map[string]json.RawMessage `json:"capabilities"`
	}
	if err := json.Unmarshal(res, &ir); err != nil {
		slog.Warn("plugin: parse initialize capabilities", "server", c.name, "err", err)
	}
	c.protocolVersion = ir.ProtocolVersion
	toolsCapability, hasTools := ir.Capabilities["tools"]
	c.capabilities.tools = hasTools
	c.capabilities.toolsListChanged = false
	if hasTools && len(toolsCapability) > 0 {
		var advertised struct {
			ListChanged bool `json:"listChanged"`
		}
		if err := json.Unmarshal(toolsCapability, &advertised); err != nil {
			slog.Warn("plugin: parse tools capability", "server", c.name, "err", err)
		} else {
			c.capabilities.toolsListChanged = advertised.ListChanged
		}
	}
	promptsCapability, hasPrompts := ir.Capabilities["prompts"]
	c.capabilities.prompts = hasPrompts
	c.capabilities.promptsListChanged = capabilityListChanged(promptsCapability)
	resourcesCapability, hasResources := ir.Capabilities["resources"]
	c.capabilities.resources = hasResources
	c.capabilities.resourcesListChanged = capabilityListChanged(resourcesCapability)
	c.capabilities.serverExtensions = parseServerExtensions(ir.Capabilities["extensions"])
	return nil
}

// parseServerExtensions reads the server's declared extension IDs from the
// initialize capabilities. MCP Apps requires two-way agreement, so the raw
// settings are kept server-side; only the ID set matters here.
func parseServerExtensions(raw json.RawMessage) map[string]bool {
	if len(raw) == 0 {
		return nil
	}
	var declared map[string]json.RawMessage
	if err := json.Unmarshal(raw, &declared); err != nil || len(declared) == 0 {
		return nil
	}
	extensions := make(map[string]bool, len(declared))
	for id := range declared {
		extensions[id] = true
	}
	return extensions
}

// elicitationUsable reports that a live negotiated session can deliver either
// legacy push-style or 2026 multi-round-trip elicitation to the client handler.
func (c *Client) elicitationUsable() bool {
	return c.protocolVersion != ""
}

func capabilityListChanged(capability json.RawMessage) bool {
	if len(capability) == 0 {
		return false
	}
	var advertised struct {
		ListChanged bool `json:"listChanged"`
	}
	return json.Unmarshal(capability, &advertised) == nil && advertised.ListChanged
}

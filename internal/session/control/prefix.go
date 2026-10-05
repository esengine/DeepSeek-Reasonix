// prefix.go — the request-side prefix a session samples against: the system
// prompt and the provider-visible tool schemas behind the cache hashes that
// usage events report.
package control

import "reasonix/internal/contract/provider"

// SystemPrompt returns the base system prompt sessions are seeded with. With
// ToolSchemas it is what the cache hashes cover, so a recorder that persists
// both can prove the pair belongs to a run's rounds instead of assuming it.
func (c *Controller) SystemPrompt() string {
	if c == nil {
		return ""
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.systemPrompt
}

// ToolSchemas returns the provider-visible schema set the executor samples
// against — the same list the prefix-cache shape is computed from.
func (c *Controller) ToolSchemas() []provider.ToolSchema {
	if c == nil {
		return nil
	}
	reg := c.mcp.registry()
	if reg == nil {
		return nil
	}
	return reg.Schemas()
}

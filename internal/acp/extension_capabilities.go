package acp

// ReasonixExtensionCapabilities advertises Reasonix-specific ACP extensions.
// ACP v1 reserves agentCapabilities._meta for vendor capability discovery.
type ReasonixExtensionCapabilities struct {
	// MCPInteraction advertises the opt-in reverse request for MCP elicitation.
	MCPInteraction *MCPInteractionCapability `json:"mcpInteraction,omitempty"`
	SessionSteer   *SessionSteerCapability   `json:"sessionSteer,omitempty"`
	// SessionInbox advertises the durable session-level instruction queue.
	SessionInbox *SessionInboxCapability `json:"sessionInbox,omitempty"`
	// SessionReloadExtensions advertises the vendor runtime-reload method.
	SessionReloadExtensions *SessionReloadExtensionsCapability `json:"sessionReloadExtensions,omitempty"`
	// ExtensionSurface advertises structured extension-UI surface support:
	// the agent publishes surfaces as vendor session/update payloads.
	ExtensionSurface *ExtensionSurfaceCapability `json:"extensionSurface,omitempty"`
}

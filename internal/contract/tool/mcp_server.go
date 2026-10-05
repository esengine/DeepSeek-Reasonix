package tool

// MCPAnnotations exposes safety-relevant annotations reported by an installed
// MCP server. These hints do not change the provider-visible tool contract;
// execution policy consumes them locally.
type MCPAnnotations interface {
	MCPDestructiveHint() bool
}

// MCPServerAuthorization reports whether the user installed this MCP server or
// authorized its exact project identity. Authorization belongs to the server,
// not to individual tools; readOnly/destructive metadata is checked separately.
type MCPServerAuthorization interface {
	MCPServerAuthorized() bool
}

// HasMCPDestructiveHint reports whether t is an MCP tool its server marked destructive.
func HasMCPDestructiveHint(t Tool) bool {
	annotations, ok := t.(MCPAnnotations)
	return ok && annotations.MCPDestructiveHint()
}

// IsMCPServerAuthorized reports whether t belongs to an MCP server the user authorized.
func IsMCPServerAuthorized(t Tool) bool {
	authority, ok := t.(MCPServerAuthorization)
	return ok && authority.MCPServerAuthorized()
}

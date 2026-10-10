package config

// UserOwned is a config the user keeps in their own home. A project's config,
// its .mcp.json and an installed package are someone else's, and none of them
// may relax a check for whoever opens or installs them.
func (s MCPConfigSource) UserOwned() bool {
	switch s {
	case MCPSourceUserConfig, MCPSourceLegacyUser, MCPSourceClaudeUser, MCPSourceClaudeLocal:
		return true
	default:
		return false
	}
}

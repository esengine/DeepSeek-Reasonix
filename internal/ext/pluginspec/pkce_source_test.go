package pluginspec

import (
	"testing"

	"reasonix/internal/contract/config"
)

// Only a config the user keeps in their own home may relax the PKCE check. A
// project's config, its .mcp.json and an installed package are written by
// someone else, and the flag in them is ignored.
func TestPKCEOptOutIsHonouredOnlyFromTheUsersOwnConfig(t *testing.T) {
	cases := map[config.MCPConfigSource]bool{
		config.MCPSourceUserConfig:     true,
		config.MCPSourceLegacyUser:     true,
		config.MCPSourceClaudeUser:     true,
		config.MCPSourceClaudeLocal:    true,
		config.MCPSourceProjectConfig:  false,
		config.MCPSourceProjectMCPJSON: false,
		config.MCPSourcePluginPackage:  false,
		config.MCPSourceUnknown:        false,
	}
	for source, want := range cases {
		spec := FromEntry(config.PluginEntry{
			Name: "remote", Type: "http", URL: "https://mcp.example.test/mcp",
			Source: source, OAuthAllowMissingPKCEMetadata: true,
		}, "", Options{})
		if spec.OAuthAllowMissingPKCEMetadata != want {
			t.Errorf("source %q: opt-out honoured = %v, want %v", source, spec.OAuthAllowMissingPKCEMetadata, want)
		}
	}
}

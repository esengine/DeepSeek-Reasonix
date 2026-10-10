package config

import "testing"

func TestUserOwnedSourcesAreTheUsersOwnFiles(t *testing.T) {
	for source, want := range map[MCPConfigSource]bool{
		MCPSourceUserConfig: true, MCPSourceLegacyUser: true, MCPSourceClaudeUser: true, MCPSourceClaudeLocal: true,
		MCPSourceProjectConfig: false, MCPSourceProjectMCPJSON: false, MCPSourcePluginPackage: false, MCPSourceUnknown: false,
	} {
		if got := source.UserOwned(); got != want {
			t.Errorf("%q.UserOwned() = %v, want %v", source, got, want)
		}
	}
}

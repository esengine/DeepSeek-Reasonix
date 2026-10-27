package config

import "testing"

func TestAProjectFolderCannotPutItsOwnServerInTheProviderSchema(t *testing.T) {
	cfg := &Config{}
	for _, src := range []MCPConfigSource{MCPSourceProjectConfig, MCPSourceProjectMCPJSON} {
		entry := PluginEntry{Name: "repo-server", Load: MCPLoadAlways, Source: src}
		if cfg.MCPAlwaysLoad(entry) {
			t.Errorf("a server declared by %s reached the provider schema on its own say-so", src)
		}
	}
	for _, src := range []MCPConfigSource{MCPSourceUserConfig, MCPSourceUnknown, MCPSourcePluginPackage} {
		if !cfg.MCPAlwaysLoad(PluginEntry{Name: "mine", Load: MCPLoadAlways, Source: src}) {
			t.Errorf("a server the user declared (%q) lost its load mode", src)
		}
	}
	if err := cfg.SetMCPLoad("repo-server", MCPLoadAlways); err != nil {
		t.Fatal(err)
	}
	if !cfg.MCPAlwaysLoad(PluginEntry{Name: "repo-server", Load: MCPLoadAlways, Source: MCPSourceProjectConfig}) {
		t.Error("the user's own [tools.mcp_load] choice must still put a project server in the schema")
	}
}

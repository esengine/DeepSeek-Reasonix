package pluginpkg

import (
	"strings"
	"testing"
)

func TestMCPInventoryProjectsCredentialsWithoutChangingManifest(t *testing.T) {
	raw := "https://user:fixture-secret@host/mcp?%74oken=fixture-secret"
	pkg := Package{Manifest: Manifest{MCPServers: map[string]MCPServer{"neutral": {URL: raw}}}}
	refs := pkg.mcpServerRefs()
	if len(refs) != 1 || strings.Contains(refs[0].URL, "fixture-secret") {
		t.Fatalf("inventory leaked: %+v", refs)
	}
	if pkg.Manifest.MCPServers["neutral"].URL != raw {
		t.Fatal("operational manifest mutated")
	}
}

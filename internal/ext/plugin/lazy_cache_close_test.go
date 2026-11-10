package plugin

import (
	"testing"

	"reasonix/internal/base/testenv"
)

func TestLazyCacheWriteRetainsHandshakeAfterHostClose(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	spec := Spec{Name: "docs", Type: "http", URL: "https://example.test/mcp"}
	host := resourceHost(&resourceTransport{}, true)
	host.clients[0].hasTools = true
	host.clients[0].hasPrompts = true
	host.clients[0].instructions = "Declared instructions"
	spawn := &lazySpawn{host: host, spec: spec}
	write := spawn.cacheWrite(nil)
	host.Close()
	write()
	cache, ok := LoadCachedSchemaForSpec(spec)
	if !ok || !cache.Capabilities["resources"] || !cache.Capabilities["prompts"] || !cache.Capabilities["tools"] || cache.Instructions != "Declared instructions" {
		t.Fatalf("shutdown changed the cached handshake: %+v", cache)
	}
}

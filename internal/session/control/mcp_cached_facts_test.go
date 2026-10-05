package control

import (
	"encoding/json"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/ext/plugin"
)

func factsSpec() plugin.Spec {
	return plugin.Spec{Name: "atlas", Type: "stdio", Command: "atlas-mcp", Args: []string{"--serve"}}
}

func saveFacts(t *testing.T, key string) {
	t.Helper()
	if err := plugin.SaveCachedSchema("atlas", plugin.CachedSchema{
		CacheKey:     key,
		Instructions: "Maps a repository's symbols.",
		Tools: []plugin.CachedTool{{
			Name:        "find_symbol",
			Description: "Locate a symbol by name.",
			Schema:      json.RawMessage(`{"type":"object"}`),
			ReadOnly:    true,
		}},
	}); err != nil {
		t.Fatalf("SaveCachedSchema: %v", err)
	}
}

// A server that is switched off, or lazy and not needed yet, has no live
// connection to ask — and the settings row that lists it still has to say what
// it is. The last handshake wrote that answer down; this is it being read back.
func TestCachedFactsAnswerForAServerThatIsNotConnected(t *testing.T) {
	t.Setenv("REASONIX_CACHE_HOME", testenv.TempDir(t))
	spec := factsSpec()
	saveFacts(t, plugin.SchemaCacheKey(spec))

	desc, tools, stale := mcpCachedFacts(spec)
	if desc != "Maps a repository's symbols." {
		t.Fatalf("description = %q, want the server's own words", desc)
	}
	if len(tools) != 1 || tools[0].Name != "find_symbol" || tools[0].Description != "Locate a symbol by name." {
		t.Fatalf("tools = %+v, want the cached tool with its description", tools)
	}
	if !tools[0].ReadOnlyHint {
		t.Error("the read-only hint did not survive the cache round trip")
	}
	if stale {
		t.Error("a matching cache key was reported stale")
	}
}

// A declaration edited since the cache was written does not make the recorded
// answer worthless, only unproven. Hiding it would leave the row blank; showing
// it unmarked would claim it is current.
func TestChangedDeclarationMarksTheCachedAnswerStale(t *testing.T) {
	t.Setenv("REASONIX_CACHE_HOME", testenv.TempDir(t))
	saveFacts(t, plugin.SchemaCacheKey(factsSpec()))

	moved := factsSpec()
	moved.Args = []string{"--serve", "--port=9000"}
	desc, tools, stale := mcpCachedFacts(moved)
	if desc == "" || len(tools) != 1 {
		t.Fatalf("a changed declaration hid the recorded answer: %q %+v", desc, tools)
	}
	if !stale {
		t.Error("the answer was presented as current after the declaration changed")
	}
}

func TestNoCacheMeansNoInventedDescription(t *testing.T) {
	t.Setenv("REASONIX_CACHE_HOME", testenv.TempDir(t))
	desc, tools, stale := mcpCachedFacts(factsSpec())
	if desc != "" || tools != nil || stale {
		t.Fatalf("mcpCachedFacts on an empty cache = %q %+v %v, want nothing", desc, tools, stale)
	}
}

// Identity is what the schema cache is keyed on, so a project the session is
// not pointed at has to build the same spec fields from its declaration alone.
func TestIdentitySpecMatchesTheKeyTheSessionWouldUse(t *testing.T) {
	entry := config.PluginEntry{Name: "atlas", Type: "stdio", Command: "atlas-mcp", Args: []string{"--serve"}}
	if got := plugin.SchemaCacheKey(mcpIdentitySpec(entry, "")); got != plugin.SchemaCacheKey(factsSpec()) {
		t.Fatalf("identity key = %q, want the key the live spec produces", got)
	}
}

func TestDisabledToolsStayOutOfMCPStatusFromStaleCache(t *testing.T) {
	t.Setenv("REASONIX_CACHE_HOME", testenv.TempDir(t))
	saveFacts(t, plugin.SchemaCacheKey(factsSpec()))
	entry := config.PluginEntry{Name: "atlas", Command: "atlas-mcp", Args: []string{"--serve"}, DisabledTools: []string{"find_symbol"}}
	desc, tools, stale := mcpCachedFacts(mcpIdentitySpec(entry, ""))
	if desc == "" || len(tools) != 0 || !stale {
		t.Fatalf("filtered stale facts = %q %+v %v", desc, tools, stale)
	}
}

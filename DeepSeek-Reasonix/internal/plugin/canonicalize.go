package plugin

import (
	"encoding/json"
	"sort"

	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// sortToolsByName returns a new slice of tools sorted alphabetically by Name().
func sortToolsByName(tools []tool.Tool) []tool.Tool {
	sorted := make([]tool.Tool, len(tools))
	copy(sorted, tools)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].Name() < sorted[j].Name()
	})
	return sorted
}

// canonicalizeSchema recursively stabilizes a JSON Schema so the same logical
// schema always produces the same byte representation — important for cache
// fingerprint stability across MCP sessions.
func canonicalizeSchema(raw json.RawMessage) json.RawMessage {
	return provider.CanonicalizeSchema(raw)
}

package agent

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"

	"reasonix/internal/event"
	"reasonix/internal/provider"
)

// PrefixShape hashes the portions of the request prefix that influence
// provider-side prompt-cache reuse. Comparing snapshots across turns
// lets us explain *why* a cache miss happened.
type PrefixShape struct {
	SystemHash        string
	ToolsHash         string
	PrefixHash        string
	LogRewriteVersion int
	ToolSchemaTokens  int
}

// CacheDiagnostics is a type alias for event.CacheDiagnostics so the agent
// can construct and compare diagnostics without importing event itself in
// every call site, while still assigning to event.Event.CacheDiagnostics.
type CacheDiagnostics = event.CacheDiagnostics

func shortHash(v interface{}) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return fmt.Sprintf("%x", h[:8])
}

// CaptureShape takes a snapshot of the current prefix state.
func CaptureShape(systemPrompt string, schemas []provider.ToolSchema, rewriteVersion int) PrefixShape {
	normalizedSchemas := normalizeToolSchemas(schemas)
	toolsJSON, _ := json.Marshal(normalizedSchemas)
	return PrefixShape{
		SystemHash: shortHash(systemPrompt),
		ToolsHash:  shortHash(string(toolsJSON)),
		PrefixHash: shortHash(map[string]interface{}{
			"system": systemPrompt,
			"tools":  string(toolsJSON),
		}),
		LogRewriteVersion: rewriteVersion,
		ToolSchemaTokens:  estimateTokens(string(toolsJSON)),
	}
}

func normalizeToolSchemas(schemas []provider.ToolSchema) []provider.ToolSchema {
	out := make([]provider.ToolSchema, len(schemas))
	copy(out, schemas)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		if out[i].Description != out[j].Description {
			return out[i].Description < out[j].Description
		}
		return string(out[i].Parameters) < string(out[j].Parameters)
	})
	return out
}

// CompareShape returns diagnostics describing what changed between two shapes.
func CompareShape(prev, cur PrefixShape, usage *provider.Usage) CacheDiagnostics {
	reasons := []string{}
	if prev.SystemHash != "" && prev.SystemHash != cur.SystemHash {
		reasons = append(reasons, "system")
	}
	if prev.ToolsHash != "" && prev.ToolsHash != cur.ToolsHash {
		reasons = append(reasons, "tools")
	}
	if prev.LogRewriteVersion != cur.LogRewriteVersion {
		reasons = append(reasons, "log_rewrite")
	}
	var miss, hit int
	if usage != nil {
		miss = usage.CacheMissTokens
		hit = usage.CacheHitTokens
	}
	return CacheDiagnostics{
		PrefixHash:          cur.PrefixHash,
		PrefixChanged:       len(reasons) > 0,
		PrefixChangeReasons: reasons,
		SystemHash:          cur.SystemHash,
		ToolsHash:           cur.ToolsHash,
		LogRewriteVersion:   cur.LogRewriteVersion,
		ToolSchemaTokens:    cur.ToolSchemaTokens,
		CacheMissTokens:     miss,
		CacheHitTokens:      hit,
	}
}

// estimateTokens gives a rough token count from byte length.
// A proper tokenizer would be more accurate, but for diagnostic
// purposes a byte-based estimate is sufficient and zero-alloc.
func estimateTokens(s string) int {
	// ~4 chars per token is a workable heuristic for code-heavy JSON.
	if len(s) == 0 {
		return 0
	}
	return len(s) / 4
}

// SchemaTokenCosts returns per-tool token cost estimates for display.
func SchemaTokenCosts(schemas []provider.ToolSchema) []ToolSchemaCost {
	out := make([]ToolSchemaCost, 0, len(schemas))
	for _, s := range schemas {
		b, _ := json.Marshal(s)
		out = append(out, ToolSchemaCost{Name: s.Name, Tokens: estimateTokens(string(b))})
	}
	return out
}

// ToolSchemaCost is a per-tool token cost estimate for diagnostic display.
type ToolSchemaCost struct {
	Name   string
	Tokens int
}

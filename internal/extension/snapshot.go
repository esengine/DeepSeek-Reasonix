package extension

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"maps"
	"sort"

	"reasonix/internal/provider"
)

// RuntimeSnapshot is the frozen, effective runtime configuration produced by
// one Builder run. It is immutable after Freeze: fields are private and every
// accessor returns defensive copies, so snapshots can be shared across turns,
// subagents, and frontends without locking. Two snapshots with equal
// CacheHash are interchangeable for provider caching purposes even if their
// provenance differs.
type RuntimeSnapshot struct {
	generation       uint64
	catalog          *Catalog
	systemPrompt     string
	toolSchemas      []provider.ToolSchema
	interceptorChain map[InterceptorPoint][]Contribution
	replacements     map[Slot]ContributionSource
	diagnostics      []string
	cacheHash        string
	systemHash       string
	toolsHash        string
}

// Generation returns the build generation. Generations let stale cleanup
// (RuntimeSet.CloseIfGeneration) recognize that a snapshot has been
// superseded.
func (s *RuntimeSnapshot) Generation() uint64 { return s.generation }

// Catalog returns the frozen catalog of effective (post-resolution)
// contributions. The catalog itself is immutable; its accessors return
// copies.
func (s *RuntimeSnapshot) Catalog() *Catalog { return s.catalog }

// SystemPrompt returns the assembled system prompt text.
func (s *RuntimeSnapshot) SystemPrompt() string { return s.systemPrompt }

// ToolSchemas returns the canonical provider-visible tool schemas, sorted by
// name. The slice is a copy — mutating it cannot affect the snapshot.
func (s *RuntimeSnapshot) ToolSchemas() []provider.ToolSchema {
	out := make([]provider.ToolSchema, len(s.toolSchemas))
	copy(out, s.toolSchemas)
	return out
}

// InterceptorChain returns the ordered interceptor chain per point (a deep
// copy). Points with no interceptors are absent, not empty.
func (s *RuntimeSnapshot) InterceptorChain() map[InterceptorPoint][]Contribution {
	out := make(map[InterceptorPoint][]Contribution, len(s.interceptorChain))
	for point, chain := range s.interceptorChain {
		cp := make([]Contribution, len(chain))
		copy(cp, chain)
		out[point] = cp
	}
	return out
}

// Replacements returns the winning owner per replacement slot (a copy).
func (s *RuntimeSnapshot) Replacements() map[Slot]ContributionSource {
	out := make(map[Slot]ContributionSource, len(s.replacements))
	maps.Copy(out, s.replacements)
	return out
}

// Diagnostics returns human-readable notes about the assembly — currently the
// shadowing disputes a ConflictCollect build resolved with its ordinary
// winner rules instead of failing. Each entry names the kind, the canonical
// ID, and every source that claimed it. The slice is a copy; empty means the
// build resolved cleanly.
func (s *RuntimeSnapshot) Diagnostics() []string {
	out := make([]string, len(s.diagnostics))
	copy(out, s.diagnostics)
	return out
}

// CacheHash returns the fingerprint of the provider-visible prefix state.
func (s *RuntimeSnapshot) CacheHash() string { return s.cacheHash }

// CacheShape returns the two halves of CacheHash — the system-prompt hash and
// the tool-schemas hash — so cache-miss diagnostics can say which half moved
// without re-hashing.
func (s *RuntimeSnapshot) CacheShape() (systemHash, toolsHash string) {
	return s.systemHash, s.toolsHash
}

// normalizeToolSchemas sorts schemas by (name, description, parameters) so a
// reordered input cannot change the hash. This mirrors the canonicalization
// in internal/agent/cache_shape.go; it is reimplemented here rather than
// imported because the kernel must stay below the agent package in the
// dependency graph.
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

// cacheHashInput is the canonical hashed form: JSON object keys in
// declaration order, schemas pre-sorted, parameters pre-canonicalized at
// assemble time (provider.CanonicalizeSchema sorts schema keys, so the raw
// bytes are stable across processes).
type cacheHashInput struct {
	SystemPrompt string                `json:"systemPrompt"`
	ToolSchemas  []provider.ToolSchema `json:"toolSchemas"`
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// computeCacheShape hashes the system prompt and the canonical tool schemas.
// Keeping the two halves separate costs nothing and lets CacheShape explain
// prefix-cache misses the way internal/agent.CompareShape does.
func computeCacheShape(systemPrompt string, schemas []provider.ToolSchema) (systemHash, toolsHash, cacheHash string) {
	canonical := normalizeToolSchemas(schemas)
	toolsJSON, _ := json.Marshal(canonical)
	systemHash = sha256Hex([]byte(systemPrompt))
	toolsHash = sha256Hex(toolsJSON)
	combined, _ := json.Marshal(cacheHashInput{SystemPrompt: systemPrompt, ToolSchemas: canonical})
	cacheHash = sha256Hex(combined)
	return systemHash, toolsHash, cacheHash
}

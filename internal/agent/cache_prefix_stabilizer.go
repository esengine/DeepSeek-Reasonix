package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
	"sync"
)

// PrefixStabilizerStats holds statistics about cache prefix stabilization.
type PrefixStabilizerStats struct {
	StabilizedCount  int
	PreventedChanges int
	TotalTokensSaved int
}

// CachePrefixStabilizer stabilizes cache prefixes by detecting and preventing
// unnecessary changes to the system prompt and tools schema. By normalizing
// whitespace and sorting tool definitions deterministically, it ensures that
// semantically equivalent prefixes produce identical cache keys.
type CachePrefixStabilizer struct {
	mu               sync.RWMutex
	stabilizedCount  int
	preventedChanges int
	lastStableHash   string
	totalTokensSaved int
}

// NewCachePrefixStabilizer creates a new CachePrefixStabilizer.
func NewCachePrefixStabilizer() *CachePrefixStabilizer {
	return &CachePrefixStabilizer{}
}

// Stabilize returns stabilized versions of systemPrompt and toolsSchema.
// It normalizes whitespace and sorts tool definitions alphabetically by name
// to ensure deterministic order for cache prefix stability.
//
// If the stabilized output is identical to the previous call (i.e. the raw
// input only differed in whitespace or tool ordering), a "prevented change"
// is recorded because without stabilization the cache prefix would have
// changed unnecessarily.
func (c *CachePrefixStabilizer) Stabilize(systemPrompt string, toolsSchema string) (string, string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Normalize whitespace in system prompt
	stabilizedPrompt := cpsNormalizeWhitespace(systemPrompt)

	// Normalize and sort tool definitions in tools schema
	stabilizedTools := cpsStabilizeToolsSchema(toolsSchema)

	// Compute hash of the stabilized content
	hash := cpsComputeHash(stabilizedPrompt + "\n" + stabilizedTools)

	// Check if the stabilized output changed compared to the last stable hash
	if c.lastStableHash != "" && hash == c.lastStableHash {
		// The raw input may have changed but the stabilized output is the
		// same, meaning we prevented an unnecessary cache prefix change.
		c.preventedChanges++
		c.totalTokensSaved += cpsEstimatedTokensSavedPerPrevention
	} else {
		// The stabilized output genuinely changed (or this is the first call).
		c.stabilizedCount++
		c.lastStableHash = hash
	}

	return stabilizedPrompt, stabilizedTools
}

// DetectChange returns true if currentHash differs from the last stable hash.
func (c *CachePrefixStabilizer) DetectChange(currentHash string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return currentHash != c.lastStableHash
}

// GetStats returns statistics about the cache prefix stabilizer.
func (c *CachePrefixStabilizer) GetStats() PrefixStabilizerStats {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return PrefixStabilizerStats{
		StabilizedCount:  c.stabilizedCount,
		PreventedChanges: c.preventedChanges,
		TotalTokensSaved: c.totalTokensSaved,
	}
}

// Reset resets all statistics and state.
func (c *CachePrefixStabilizer) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stabilizedCount = 0
	c.preventedChanges = 0
	c.lastStableHash = ""
	c.totalTokensSaved = 0
}

// ---------------------------------------------------------------------------
// Internal helpers (prefixed with cps to avoid naming conflicts)
// ---------------------------------------------------------------------------

// cpsEstimatedTokensSavedPerPrevention is a rough estimate of tokens saved
// when a cache prefix change is prevented. Cache prefix changes typically
// invalidate cached prompt tokens, so preventing one saves roughly the
// system-prompt + tool-schema token count.
const cpsEstimatedTokensSavedPerPrevention = 100

// cpsNormalizeWhitespace collapses multiple whitespace characters into single
// spaces and trims leading/trailing whitespace.
func cpsNormalizeWhitespace(s string) string {
	fields := strings.Fields(s)
	return strings.Join(fields, " ")
}

// cpsStabilizeToolsSchema normalizes whitespace and sorts tool definitions
// alphabetically by name to ensure deterministic ordering.
func cpsStabilizeToolsSchema(toolsSchema string) string {
	normalized := cpsNormalizeWhitespace(toolsSchema)

	// If the schema looks like a JSON array, try to sort tool entries by name
	trimmed := strings.TrimSpace(normalized)
	if !strings.HasPrefix(trimmed, "[") || !strings.HasSuffix(trimmed, "]") {
		return normalized
	}

	inner := trimmed[1 : len(trimmed)-1]
	objects := cpsSplitJSONObjects(inner)
	if len(objects) <= 1 {
		return normalized
	}

	sort.Slice(objects, func(i, j int) bool {
		return cpsExtractToolName(objects[i]) < cpsExtractToolName(objects[j])
	})

	return "[" + strings.Join(objects, ",") + "]"
}

// cpsSplitJSONObjects splits a string containing multiple JSON objects into
// individual object strings by tracking brace depth. This handles nested
// objects but does not account for braces inside string literals.
func cpsSplitJSONObjects(s string) []string {
	var objects []string
	depth := 0
	start := -1

	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch ch {
		case '{':
			if depth == 0 {
				start = i
			}
			depth++
		case '}':
			depth--
			if depth == 0 && start >= 0 {
				objects = append(objects, s[start:i+1])
				start = -1
			}
		}
	}

	return objects
}

// cpsExtractToolName extracts the "name" field value from a JSON tool object
// string. If the name cannot be found, the full object string is returned as
// a fallback so that sorting remains deterministic.
func cpsExtractToolName(obj string) string {
	idx := strings.Index(obj, `"name"`)
	if idx == -1 {
		return obj
	}
	rest := obj[idx:]
	colonIdx := strings.Index(rest, ":")
	if colonIdx == -1 {
		return obj
	}
	rest = rest[colonIdx+1:]
	rest = strings.TrimLeft(rest, " ")
	startQuote := strings.Index(rest, `"`)
	if startQuote == -1 {
		return obj
	}
	rest = rest[startQuote+1:]
	endQuote := strings.Index(rest, `"`)
	if endQuote == -1 {
		return obj
	}
	return rest[:endQuote]
}

// cpsComputeHash computes a SHA-256 hash of the given content and returns
// the hex-encoded string.
func cpsComputeHash(content string) string {
	h := sha256.Sum256([]byte(content))
	return hex.EncodeToString(h[:])
}

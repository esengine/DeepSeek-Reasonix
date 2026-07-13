package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"
)

// CachedToolOutput represents a cached tool output entry.
// OPT-64: Tool output caching to avoid re-running identical tool calls.
type CachedToolOutput struct {
	Output    string
	ToolName  string
	CreatedAt int64
	HitCount  int
}

// ToolOutputCache caches tool outputs to avoid re-running identical tool calls
// with the same arguments.
type ToolOutputCache struct {
	mu           sync.RWMutex
	cache        map[string]*CachedToolOutput
	maxSize      int
	totalHits    int
	totalMisses  int
	tokensSaved  int
}

// ToolOutputCacheStats holds statistics about tool output cache performance.
type ToolOutputCacheStats struct {
	TotalHits   int
	TotalMisses int
	TokensSaved int
	CacheSize   int
	HitRate     float64
}

// NewToolOutputCache creates a new ToolOutputCache with the given maximum size.
// If maxSize is <= 0, a default size of 100 is used.
func NewToolOutputCache(maxSize int) *ToolOutputCache {
	if maxSize <= 0 {
		maxSize = 100
	}
	return &ToolOutputCache{
		cache:   make(map[string]*CachedToolOutput),
		maxSize: maxSize,
	}
}

// toolCacheKey generates a unique cache key from the tool name and arguments.
func toolCacheKey(toolName, args string) string {
	h := sha256.Sum256([]byte(toolName + ":" + args))
	return hex.EncodeToString(h[:])
}

// Get retrieves a cached tool output by tool name and arguments. Returns the
// output and true if found, or empty string and false if not cached.
func (c *ToolOutputCache) Get(toolName string, args string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := toolCacheKey(toolName, args)
	entry, exists := c.cache[key]
	if !exists {
		c.totalMisses++
		return "", false
	}

	c.totalHits++
	entry.HitCount++
	c.tokensSaved += len(entry.Output) / 4
	return entry.Output, true
}

// Set stores a tool output in the cache. If the cache is full, the oldest entry
// is evicted.
func (c *ToolOutputCache) Set(toolName string, args string, output string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := toolCacheKey(toolName, args)

	if entry, exists := c.cache[key]; exists {
		entry.Output = output
		entry.CreatedAt = time.Now().Unix()
		entry.HitCount = 0
		return
	}

	if len(c.cache) >= c.maxSize {
		var oldestKey string
		var oldestTime int64
		first := true
		for k, v := range c.cache {
			if first || v.CreatedAt < oldestTime {
				oldestKey = k
				oldestTime = v.CreatedAt
				first = false
			}
		}
		if oldestKey != "" {
			delete(c.cache, oldestKey)
		}
	}

	c.cache[key] = &CachedToolOutput{
		Output:    output,
		ToolName:  toolName,
		CreatedAt: time.Now().Unix(),
		HitCount:  0,
	}
}

// Invalidate removes all cached entries for the given tool name.
func (c *ToolOutputCache) Invalidate(toolName string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for key, entry := range c.cache {
		if entry.ToolName == toolName {
			delete(c.cache, key)
		}
	}
}

// GetStats returns current statistics about the tool output cache.
func (c *ToolOutputCache) GetStats() ToolOutputCacheStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	total := c.totalHits + c.totalMisses
	hitRate := 0.0
	if total > 0 {
		hitRate = float64(c.totalHits) / float64(total)
	}

	return ToolOutputCacheStats{
		TotalHits:   c.totalHits,
		TotalMisses: c.totalMisses,
		TokensSaved: c.tokensSaved,
		CacheSize:   len(c.cache),
		HitRate:     hitRate,
	}
}

// Reset clears all cached entries and statistics.
func (c *ToolOutputCache) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.cache = make(map[string]*CachedToolOutput)
	c.totalHits = 0
	c.totalMisses = 0
	c.tokensSaved = 0
}

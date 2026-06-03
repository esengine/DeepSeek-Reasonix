package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
)

// toolCacheSize is the maximum number of read-only tool results kept in the
// LRU cache. 64 entries is enough to cover a typical session's repeated
// read_file/grep/ls calls without consuming significant memory (each entry
// is at most maxToolOutputBytes = 32KB, so worst case ~2MB).
const toolCacheSize = 64

// toolCache is a simple LRU cache for read-only tool outputs. When the model
// calls the same read-only tool with the same arguments (e.g. re-reading a
// file it already read), the cached result is returned without re-executing.
// This saves both the tool's execution time and the context tokens that would
// be spent on a duplicate result.
//
// The cache is keyed by (toolName, sha256(args)) to avoid storing large
// argument strings. It is safe for concurrent use (tool calls can parallelise).
type toolCache struct {
	mu    sync.Mutex
	order []string              // LRU order: newest at end
	store map[string]cacheEntry // key → entry
}

type cacheEntry struct {
	output string
}

func newToolCache() *toolCache {
	return &toolCache{
		order: make([]string, 0, toolCacheSize),
		store: make(map[string]cacheEntry, toolCacheSize),
	}
}

// cacheKey builds a deterministic cache key from the tool name and arguments.
// The arguments are hashed to keep key size bounded.
func cacheKey(toolName, args string) string {
	h := sha256.Sum256([]byte(args))
	return toolName + ":" + hex.EncodeToString(h[:8])
}

// Get returns the cached output for (toolName, args), or ("", false) on miss.
func (c *toolCache) Get(toolName, args string) (string, bool) {
	key := cacheKey(toolName, args)
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.store[key]
	if !ok {
		return "", false
	}
	// Move to end (most recently used).
	c.touchLocked(key)
	return e.output, true
}

// Put stores a tool result in the cache. If the cache is full, the least
// recently used entry is evicted.
func (c *toolCache) Put(toolName, args, output string) {
	key := cacheKey(toolName, args)
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.store[key]; exists {
		c.store[key] = cacheEntry{output: output}
		c.touchLocked(key)
		return
	}
	// Evict oldest if at capacity.
	for len(c.order) >= toolCacheSize {
		evict := c.order[0]
		c.order = c.order[1:]
		delete(c.store, evict)
	}
	c.order = append(c.order, key)
	c.store[key] = cacheEntry{output: output}
}

// touchLocked moves key to the end of the LRU order (most recently used).
func (c *toolCache) touchLocked(key string) {
	for i, k := range c.order {
		if k == key {
			c.order = append(c.order[:i], c.order[i+1:]...)
			c.order = append(c.order, key)
			return
		}
	}
}

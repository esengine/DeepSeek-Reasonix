package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"
)

// CachedFragment represents a cached reusable prompt fragment.
// OPT-65: Prompt fragment caching by hash to avoid redundant token generation.
type CachedFragment struct {
	Content      string
	Hash         string
	TokenEstimate int
	HitCount     int
	LastUsed     int64
}

// PromptFragmentCache caches reusable prompt fragments such as system
// instructions and tool descriptions, keyed by a string identifier.
type PromptFragmentCache struct {
	mu           sync.RWMutex
	fragments    map[string]*CachedFragment
	totalHits    int
	totalMisses  int
	tokensSaved  int
}

// FragmentCacheStats holds statistics about prompt fragment cache performance.
type FragmentCacheStats struct {
	TotalHits     int
	TotalMisses   int
	TokensSaved   int
	FragmentsCached int
}

// NewPromptFragmentCache creates a new PromptFragmentCache.
func NewPromptFragmentCache() *PromptFragmentCache {
	return &PromptFragmentCache{
		fragments: make(map[string]*CachedFragment),
	}
}

// fragmentHash computes the SHA-256 hash of the given content.
func fragmentHash(content string) string {
	h := sha256.Sum256([]byte(content))
	return hex.EncodeToString(h[:])
}

// estimateFragmentTokens provides a rough token estimate based on content length.
func estimateFragmentTokens(content string) int {
	return len(content) / 4
}

// GetFragment retrieves a cached prompt fragment by key. Returns the content
// and true if found, or empty string and false if not cached.
func (c *PromptFragmentCache) GetFragment(key string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	fragment, exists := c.fragments[key]
	if !exists {
		c.totalMisses++
		return "", false
	}

	c.totalHits++
	fragment.HitCount++
	fragment.LastUsed = time.Now().Unix()
	c.tokensSaved += fragment.TokenEstimate
	return fragment.Content, true
}

// SetFragment stores a prompt fragment in the cache, computing its hash and
// token estimate.
func (c *PromptFragmentCache) SetFragment(key string, content string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.fragments[key] = &CachedFragment{
		Content:       content,
		Hash:          fragmentHash(content),
		TokenEstimate: estimateFragmentTokens(content),
		HitCount:      0,
		LastUsed:      time.Now().Unix(),
	}
}

// GetOrCompute returns the cached fragment for the given key if it exists,
// otherwise calls computeFn to generate the content, caches it, and returns it.
func (c *PromptFragmentCache) GetOrCompute(key string, computeFn func() string) string {
	c.mu.Lock()
	defer c.mu.Unlock()

	fragment, exists := c.fragments[key]
	if exists {
		c.totalHits++
		fragment.HitCount++
		fragment.LastUsed = time.Now().Unix()
		c.tokensSaved += fragment.TokenEstimate
		return fragment.Content
	}

	c.totalMisses++
	content := computeFn()
	c.fragments[key] = &CachedFragment{
		Content:       content,
		Hash:          fragmentHash(content),
		TokenEstimate: estimateFragmentTokens(content),
		HitCount:      0,
		LastUsed:      time.Now().Unix(),
	}
	return content
}

// GetStats returns current statistics about the prompt fragment cache.
func (c *PromptFragmentCache) GetStats() FragmentCacheStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return FragmentCacheStats{
		TotalHits:     c.totalHits,
		TotalMisses:   c.totalMisses,
		TokensSaved:   c.tokensSaved,
		FragmentsCached: len(c.fragments),
	}
}

// Reset clears all cached fragments and statistics.
func (c *PromptFragmentCache) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.fragments = make(map[string]*CachedFragment)
	c.totalHits = 0
	c.totalMisses = 0
	c.tokensSaved = 0
}

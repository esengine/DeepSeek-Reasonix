package agent

import "sync"

// ── OPT-190: PromptSegmentCacheV3 (提示分段缓存 V3) ──
// 支持LRU淘汰和分段级缓存。通过 accessOrder 列表维护访问顺序，
// Get 时将键移至末尾（最近使用），Put 时若超出 maxEntries
// 则淘汰列表头部（最久未使用）的条目。
//
// 原理：在 prompt 分段缓存场景下，缓存空间有限，需要合理的淘汰
// 策略来保持高命中率。LRU（最近最少使用）策略保留高频访问的
// 分段，淘汰长时间未访问的分段，适应访问模式的变化。
//
// 效果：在有限缓存空间下最大化命中率，减少重复 prompt 分段的
// token 开销，提升响应速度。

// CacheV3Entry 缓存 V3 条目。
type CacheV3Entry struct {
	Content     string // 缓存内容
	Tokens      int    // token 数量
	AccessCount int    // 访问计数
}

// PromptSegmentCacheV3 提示分段缓存 V3，支持LRU淘汰和分段级缓存。
type PromptSegmentCacheV3 struct {
	mu          sync.RWMutex
	entries     map[string]CacheV3Entry
	accessOrder []string // LRU 访问顺序，头部为最久未访问
	maxEntries  int
	hits        int
	misses      int
}

// NewPromptSegmentCacheV3 创建一个新的提示分段缓存 V3。
// maxEntries 指定最大缓存条目数，超出时按 LRU 策略淘汰。
func NewPromptSegmentCacheV3(maxEntries int) *PromptSegmentCacheV3 {
	return &PromptSegmentCacheV3{
		entries:     make(map[string]CacheV3Entry),
		accessOrder: make([]string, 0),
		maxEntries:  maxEntries,
		hits:        0,
		misses:      0,
	}
}

// Get 获取缓存项，更新访问计数和LRU顺序。
// 命中时递增 hits 和 AccessCount，将键移至 accessOrder 末尾（最近使用）；
// 未命中时递增 misses，返回零值和 false。
func (c *PromptSegmentCacheV3) Get(key string) (CacheV3Entry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok {
		c.misses++
		return CacheV3Entry{}, false
	}
	c.hits++
	entry.AccessCount++
	c.entries[key] = entry
	// 更新 LRU 顺序：移到末尾（最近访问）
	c.accessOrder = psc3RemoveFromOrder(c.accessOrder, key)
	c.accessOrder = append(c.accessOrder, key)
	return entry, true
}

// Put 存入缓存，超过 maxEntries 时淘汰 LRU 项。
// 若键已存在则更新内容并保留原 AccessCount，同时更新 LRU 顺序；
// 若为新键且缓存已满，则淘汰 accessOrder 头部（最久未使用）的条目。
func (c *PromptSegmentCacheV3) Put(key string, content string, tokens int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	// 若已存在，更新内容并更新 LRU 顺序
	if existing, ok := c.entries[key]; ok {
		c.entries[key] = CacheV3Entry{
			Content:     content,
			Tokens:      tokens,
			AccessCount: existing.AccessCount,
		}
		c.accessOrder = psc3RemoveFromOrder(c.accessOrder, key)
		c.accessOrder = append(c.accessOrder, key)
		return
	}
	// 新键：检查是否需要淘汰
	if c.maxEntries > 0 && len(c.entries) >= c.maxEntries {
		if len(c.accessOrder) > 0 {
			lruKey := c.accessOrder[0]
			delete(c.entries, lruKey)
			c.accessOrder = c.accessOrder[1:]
		}
	}
	// 添加新条目
	c.entries[key] = CacheV3Entry{
		Content:     content,
		Tokens:      tokens,
		AccessCount: 0,
	}
	c.accessOrder = append(c.accessOrder, key)
}

// Invalidate 失效缓存项，从缓存和 LRU 顺序中移除指定键。
func (c *PromptSegmentCacheV3) Invalidate(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, key)
	c.accessOrder = psc3RemoveFromOrder(c.accessOrder, key)
}

// GetHitRate 获取缓存命中率。
// 命中率 = hits / (hits + misses)，若总访问数为 0 则返回 0。
func (c *PromptSegmentCacheV3) GetHitRate() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return psc3ComputeHitRate(c.hits, c.misses)
}

// GetStats 返回统计信息，包含 entries、maxEntries、hits、misses 和 hitRate。
func (c *PromptSegmentCacheV3) GetStats() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()
	hitRate := psc3ComputeHitRate(c.hits, c.misses)
	return map[string]interface{}{
		"entries":    len(c.entries),
		"maxEntries": c.maxEntries,
		"hits":       c.hits,
		"misses":     c.misses,
		"hitRate":    hitRate,
	}
}

// Reset 重置缓存状态，清空所有条目、访问顺序和统计计数。
func (c *PromptSegmentCacheV3) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = make(map[string]CacheV3Entry)
	c.accessOrder = make([]string, 0)
	c.hits = 0
	c.misses = 0
}

// psc3RemoveFromOrder 从 LRU 访问顺序中移除指定键（辅助函数）。
// 返回移除后的新切片；若键不存在则原样返回。
func psc3RemoveFromOrder(order []string, key string) []string {
	for i, k := range order {
		if k == key {
			return append(order[:i], order[i+1:]...)
		}
	}
	return order
}

// psc3ComputeHitRate 计算缓存命中率（辅助函数）。
// hits 为命中次数，misses 为未命中次数；
// 总数为 0 时返回 0，否则返回 hits / (hits + misses)。
func psc3ComputeHitRate(hits int, misses int) float64 {
	total := hits + misses
	if total == 0 {
		return 0
	}
	return float64(hits) / float64(total)
}

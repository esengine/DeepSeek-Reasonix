package agent

import "sync"

// ── OPT-197: CacheCoherenceManager (缓存一致性管理器) ──
// 保证多级缓存间的数据一致性，支持写入、读取、失效和同步操作。
// 通过 Invalidate 在所有级别失效指定 key，通过 Sync 将缺失的 key
// 传播到所有缓存级别，确保多级缓存的一致性。

// CacheCoherenceManager 缓存一致性管理器
type CacheCoherenceManager struct {
	mu            sync.RWMutex
	cacheLevels   map[string]map[string]string // level -> (key -> value)
	invalidations int
	propagations  int
	syncCount     int
}

// NewCacheCoherenceManager 创建一个新的缓存一致性管理器。
func NewCacheCoherenceManager() *CacheCoherenceManager {
	return &CacheCoherenceManager{
		cacheLevels: make(map[string]map[string]string),
	}
}

// Write 写入指定级别的缓存。
// 若该级别不存在则自动创建。
func (c *CacheCoherenceManager) Write(level string, key string, value string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cacheLevels[level] == nil {
		c.cacheLevels[level] = make(map[string]string)
	}
	c.cacheLevels[level][key] = value
}

// Read 读取指定级别的缓存。
// 返回 value 和是否存在的布尔值。
func (c *CacheCoherenceManager) Read(level string, key string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if levelCache, ok := c.cacheLevels[level]; ok {
		val, exists := levelCache[key]
		return val, exists
	}
	return "", false
}

// Invalidate 在所有级别失效缓存项。
// 遍历所有缓存级别，删除指定 key，每删除一次递增 invalidations。
func (c *CacheCoherenceManager) Invalidate(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, levelCache := range c.cacheLevels {
		if _, exists := levelCache[key]; exists {
			delete(levelCache, key)
			c.invalidations++
		}
	}
}

// Sync 同步所有级别缓存，返回同步的 key 数量。
// 收集所有级别中的唯一 key 及其值，将缺失的 key 传播到所有级别。
// 每次传播递增 propagations，调用结束后递增 syncCount。
func (c *CacheCoherenceManager) Sync() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 收集所有级别的唯一 key 及其值
	merged := make(map[string]string)
	for _, levelCache := range c.cacheLevels {
		for key, value := range levelCache {
			if _, exists := merged[key]; !exists {
				merged[key] = value
			}
		}
	}

	// 将缺失的 key 传播到所有级别
	for level := range c.cacheLevels {
		for key, value := range merged {
			if _, exists := c.cacheLevels[level][key]; !exists {
				c.cacheLevels[level][key] = value
				c.propagations++
			}
		}
	}

	c.syncCount++
	return len(merged)
}

// GetStats 返回管理器的统计信息。
// 包含: levelCount, invalidations, propagations, syncCount。
func (c *CacheCoherenceManager) GetStats() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return map[string]interface{}{
		"levelCount":    len(c.cacheLevels),
		"invalidations": c.invalidations,
		"propagations":  c.propagations,
		"syncCount":     c.syncCount,
	}
}

// Reset 重置管理器，清空所有缓存级别与统计信息。
func (c *CacheCoherenceManager) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.cacheLevels = make(map[string]map[string]string)
	c.invalidations = 0
	c.propagations = 0
	c.syncCount = 0
}

// ---------------------------------------------------------------------------
// 辅助函数（以 ccm 为前缀，避免命名冲突）
// ---------------------------------------------------------------------------

// ccmCountKeys 统计所有缓存级别中的唯一 key 数量。
func ccmCountKeys(levels map[string]map[string]string) int {
	seen := make(map[string]bool)
	for _, level := range levels {
		for key := range level {
			seen[key] = true
		}
	}
	return len(seen)
}

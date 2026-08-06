package agent

import "sync"

// ── OPT-262: CacheInvalidationCompactor (缓存失效压缩器) ──
// 对缓存失效 key 列表进行去重压缩，避免对同一 key 重复执行失效操作。
// 每次压缩都会记录输入量、去重后输出量以及被消除的重复量。
//
// 原理：在级联失效或批量失效场景下，多个上游可能同时上报同一 key
// 的失效请求。直接逐条处理会造成冗余写入与锁竞争。先压缩去重可
// 显著降低下游失效通道的压力。
//
// 效果：减少失效操作数量，降低缓存集群的同步开销，提升吞吐。

// CacheInvalidationCompactor 缓存失效压缩器。
type CacheInvalidationCompactor struct {
	mu              sync.RWMutex
	compactedKeys   map[string]bool
	totalCompacted  int
	totalReduced    int
	compactionCount int
}

// NewCacheInvalidationCompactor 创建一个新的缓存失效压缩器。
func NewCacheInvalidationCompactor() *CacheInvalidationCompactor {
	return &CacheInvalidationCompactor{
		compactedKeys: make(map[string]bool),
	}
}

// Compact 压缩 key 列表（去重），返回压缩后的唯一 key 列表。
// 同时累计输入量、被消除的重复量与压缩次数。
func (c *CacheInvalidationCompactor) Compact(keys []string) []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	unique := cicDeduplicate(keys)
	for _, k := range unique {
		c.compactedKeys[k] = true
	}
	c.totalCompacted += len(keys)
	c.totalReduced += len(keys) - len(unique)
	c.compactionCount++
	return unique
}

// GetCompactedCount 返回当前已压缩保留的唯一 key 数量。
func (c *CacheInvalidationCompactor) GetCompactedCount() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.compactedKeys)
}

// GetReductionRate 返回重复消除率（被消除的重复量占总输入量的比例）。
// 若总输入量为 0 则返回 0。
func (c *CacheInvalidationCompactor) GetReductionRate() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.totalCompacted == 0 {
		return 0
	}
	return float64(c.totalReduced) / float64(c.totalCompacted)
}

// GetStats 返回统计信息，包含 totalCompacted、totalReduced、
// compactionCount 和 uniqueKeys。
func (c *CacheInvalidationCompactor) GetStats() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return map[string]interface{}{
		"totalCompacted":  c.totalCompacted,
		"totalReduced":    c.totalReduced,
		"compactionCount": c.compactionCount,
		"uniqueKeys":      len(c.compactedKeys),
	}
}

// Reset 重置压缩器状态，清空已记录的 key 与所有统计计数。
func (c *CacheInvalidationCompactor) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.compactedKeys = make(map[string]bool)
	c.totalCompacted = 0
	c.totalReduced = 0
	c.compactionCount = 0
}

// cicDeduplicate 对 key 列表去重并保持首次出现的顺序（辅助函数）。
func cicDeduplicate(keys []string) []string {
	seen := make(map[string]bool, len(keys))
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		if !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	return out
}

package agent

import "sync"

// ── OPT-252: CacheInvalidationBatcher (缓存失效批处理器 / Cache Invalidation Batcher) ──
// 批量处理缓存失效请求以减少开销。使用 map 自动去重累积失效 key，
// 当批次达到阈值时一次性刷新，分摊固定开销，提升整体吞吐量。
//
// 原理：逐条发送缓存失效请求会产生大量重复的网络或锁开销。通过批处理，
// 将多个失效 key 累积（自动去重）后统一处理，分摊固定开销，提升整体吞吐量。
//
// 效果：减少缓存失效的刷新次数，统计批处理的 key 总数，
// 为缓存管理提供数据支撑。

// CacheInvalidationBatcher 缓存失效批处理器
type CacheInvalidationBatcher struct {
	mu           sync.RWMutex
	batch        map[string]bool // 当前批次的失效 key（去重）
	batchSize    int             // 批次阈值
	totalBatched int             // 累计添加的 key 总数
	totalFlushed int             // 累计刷新的 key 总数
}

// NewCacheInvalidationBatcher 创建缓存失效批处理器。
// batchSize 指定批次阈值，若 <= 0 则默认 32。
func NewCacheInvalidationBatcher(batchSize int) *CacheInvalidationBatcher {
	if batchSize <= 0 {
		batchSize = 32
	}
	return &CacheInvalidationBatcher{
		batch:     make(map[string]bool),
		batchSize: batchSize,
	}
}

// Add 添加失效 key 到当前批次。
// 返回 true 表示批次已达到 batchSize，应执行 Flush。
func (c *CacheInvalidationBatcher) Add(key string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.batch[key] = true
	c.totalBatched++
	return len(c.batch) >= c.batchSize
}

// Flush 刷新当前批次，返回所有失效 key 并清空批次。
// 累加刷新的 key 总数。
func (c *CacheInvalidationBatcher) Flush() []string {
	c.mu.Lock()
	defer c.mu.Unlock()

	keys := cibKeysToSlice(c.batch)
	c.totalFlushed += len(keys)
	c.batch = make(map[string]bool)
	return keys
}

// GetBatchCount 获取当前批次中 key 的数量。
func (c *CacheInvalidationBatcher) GetBatchCount() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.batch)
}

// GetStats 返回批处理器的统计信息。
// 包含 batchSize、currentBatchSize、totalBatched 和 totalFlushed。
func (c *CacheInvalidationBatcher) GetStats() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return map[string]interface{}{
		"batchSize":        c.batchSize,
		"currentBatchSize": len(c.batch),
		"totalBatched":     c.totalBatched,
		"totalFlushed":     c.totalFlushed,
	}
}

// Reset 重置批处理器的所有计数并清空批次（保留 batchSize 配置）。
func (c *CacheInvalidationBatcher) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.batch = make(map[string]bool)
	c.totalBatched = 0
	c.totalFlushed = 0
}

// cibKeysToSlice 将批次 map 的 key 转换为切片。
func cibKeysToSlice(m map[string]bool) []string {
	if len(m) == 0 {
		return make([]string, 0)
	}
	dst := make([]string, 0, len(m))
	for k := range m {
		dst = append(dst, k)
	}
	return dst
}

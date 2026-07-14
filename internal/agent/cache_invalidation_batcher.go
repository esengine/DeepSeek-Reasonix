package agent
import "sync"

// ── OPT-207: CacheInvalidationBatcher (缓存失效批处理器 / Cache Invalidation Batcher) ──
// 批量处理缓存失效以减少开销。将多个缓存失效请求累积到批次中，
// 当批次达到阈值时一次性刷新，减少频繁单条失效带来的性能开销。
//
// 原理：逐条发送缓存失效请求会产生大量重复的网络或锁开销。通过批处理，
// 将多个失效 key 累积后统一处理，分摊固定开销，提升整体吞吐量。
//
// 效果：减少缓存失效的刷新次数，统计批处理次数和失效 key 总数，
// 为缓存管理提供数据支撑。

// CacheInvalidationBatcher 缓存失效批处理器
type CacheInvalidationBatcher struct {
	mu               sync.RWMutex
	batch            []string // 当前批次的失效 key current batch of invalidation keys
	maxBatchSize     int      // 批次最大容量 maximum batch size
	flushCount       int      // 刷新次数 number of flushes
	totalInvalidated int      // 累计失效 key 总数 total invalidated keys
}

// NewCacheInvalidationBatcher 创建缓存失效批处理器。
// maxBatchSize 指定批次最大容量，若 <= 0 则默认 32。
func NewCacheInvalidationBatcher(maxBatchSize int) *CacheInvalidationBatcher {
	if maxBatchSize <= 0 {
		maxBatchSize = 32
	}
	return &CacheInvalidationBatcher{
		batch:        make([]string, 0, maxBatchSize),
		maxBatchSize: maxBatchSize,
	}
}

// Add 添加失效 key 到当前批次。
// 返回 true 表示批次已满，应执行 Flush。
func (c *CacheInvalidationBatcher) Add(key string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.batch = append(c.batch, key)
	return len(c.batch) >= c.maxBatchSize
}

// Flush 刷新当前批次，返回所有失效 key 并清空批次。
// 累加刷新次数和失效 key 总数。
func (c *CacheInvalidationBatcher) Flush() []string {
	c.mu.Lock()
	defer c.mu.Unlock()

	result := cibCopyBatch(c.batch)
	c.totalInvalidated += len(result)
	c.flushCount++
	c.batch = make([]string, 0, c.maxBatchSize)
	return result
}

// GetBatchSize 获取当前批次的 key 数量。
func (c *CacheInvalidationBatcher) GetBatchSize() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.batch)
}

// GetStats 返回批处理器的统计信息。
// 包含 maxBatchSize、batchSize、flushCount 和 totalInvalidated。
func (c *CacheInvalidationBatcher) GetStats() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return map[string]interface{}{
		"maxBatchSize":     c.maxBatchSize,
		"batchSize":        len(c.batch),
		"flushCount":       c.flushCount,
		"totalInvalidated": c.totalInvalidated,
	}
}

// Reset 重置批处理器的所有计数并清空批次（保留 maxBatchSize 配置）。
func (c *CacheInvalidationBatcher) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.batch = make([]string, 0, c.maxBatchSize)
	c.flushCount = 0
	c.totalInvalidated = 0
}

// cibCopyBatch 复制批次切片，返回独立副本。
func cibCopyBatch(src []string) []string {
	if len(src) == 0 {
		return make([]string, 0)
	}
	dst := make([]string, len(src))
	copy(dst, src)
	return dst
}

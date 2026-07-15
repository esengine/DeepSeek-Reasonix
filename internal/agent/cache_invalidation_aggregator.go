package agent
import "sync"

// OPT-237: CacheInvalidationAggregator — 缓存失效聚合器
// CacheInvalidationAggregator aggregates multiple cache invalidation requests for batch processing.
// It collects invalidation keys until the batch size is reached, then flushes them together,
// reducing the number of individual invalidation operations.
type CacheInvalidationAggregator struct {
	mu              sync.RWMutex
	pending         map[string]bool // 待处理的失效key集合 pending invalidation keys
	aggregatedCount int             // 累计聚合的key总数 total keys ever aggregated
	flushedCount    int             // 累计刷新的key总数 total keys ever flushed
	maxBatchSize    int             // 最大批量大小 maximum batch size
}

// NewCacheInvalidationAggregator creates a new CacheInvalidationAggregator with the given max batch size.
// NewCacheInvalidationAggregator 使用给定的最大批量大小创建新的CacheInvalidationAggregator。
func NewCacheInvalidationAggregator(maxBatchSize int) *CacheInvalidationAggregator {
	return &CacheInvalidationAggregator{
		pending:         make(map[string]bool),
		aggregatedCount: 0,
		flushedCount:    0,
		maxBatchSize:    maxBatchSize,
	}
}

// Add adds an invalidation key and returns true if a flush should be triggered.
// Add 添加失效key，返回是否应该flush。
func (a *CacheInvalidationAggregator) Add(key string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.pending[key] {
		a.pending[key] = true
		a.aggregatedCount++
	}
	return len(a.pending) >= a.maxBatchSize
}

// Flush flushes all aggregated keys and returns them as a slice, then clears the pending set.
// Flush 刷新并返回所有聚合的key。
func (a *CacheInvalidationAggregator) Flush() []string {
	a.mu.Lock()
	defer a.mu.Unlock()

	keys := ciaKeysToSlice(a.pending)
	a.flushedCount += len(keys)
	a.pending = make(map[string]bool)
	return keys
}

// GetPendingCount returns the number of pending invalidation keys.
// GetPendingCount 返回待处理的key数量。
func (a *CacheInvalidationAggregator) GetPendingCount() int {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return len(a.pending)
}

// IsFull returns true if the aggregator has reached its max batch size.
// IsFull 返回聚合器是否已满。
func (a *CacheInvalidationAggregator) IsFull() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return len(a.pending) >= a.maxBatchSize
}

// GetStats returns statistics about the aggregator.
// GetStats 返回聚合器的统计信息。
func (a *CacheInvalidationAggregator) GetStats() map[string]interface{} {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return map[string]interface{}{
		"maxBatchSize":    a.maxBatchSize,
		"pendingCount":    len(a.pending),
		"aggregatedCount": a.aggregatedCount,
		"flushedCount":    a.flushedCount,
	}
}

// Reset resets the aggregator to its initial state (preserving max batch size).
// Reset 重置聚合器到初始状态（保留最大批量大小配置）。
func (a *CacheInvalidationAggregator) Reset() {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.pending = make(map[string]bool)
	a.aggregatedCount = 0
	a.flushedCount = 0
}

// ciaKeysToSlice converts the pending map keys into a string slice.
// ciaKeysToSlice 将pending map的key转换为切片。
func ciaKeysToSlice(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

package agent

import "sync"

// ── OPT-167: CacheInvalidationTracker (缓存失效追踪器) ──
// 追踪缓存失效事件以优化缓存策略。
// 通过按原因分类统计失效次数，定位最频繁的失效来源，
// 进而针对性优化缓存前缀稳定性，提升缓存命中率。

// CacheInvalidationTracker 缓存失效追踪器
type CacheInvalidationTracker struct {
	mu                 sync.RWMutex
	invalidations      map[string]int // reason -> count
	totalInvalidations int
	lastInvalidatedKey string
	lastReason         string
}

// NewCacheInvalidationTracker 创建缓存失效追踪器
func NewCacheInvalidationTracker() *CacheInvalidationTracker {
	return &CacheInvalidationTracker{
		invalidations: make(map[string]int),
	}
}

// Record 记录一次失效事件
func (t *CacheInvalidationTracker) Record(key string, reason string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.invalidations[reason]++
	t.totalInvalidations++
	t.lastInvalidatedKey = key
	t.lastReason = reason
}

// GetInvalidationCount 获取特定原因的失效次数
func (t *CacheInvalidationTracker) GetInvalidationCount(reason string) int {
	t.mu.RLock()
	defer t.mu.RUnlock()

	return t.invalidations[reason]
}

// GetTopReasons 获取失效次数最多的 N 个原因（按次数降序，同频按字母序）
func (t *CacheInvalidationTracker) GetTopReasons(n int) []string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	return citSortReasons(t.invalidations, n)
}

// GetStats 返回缓存失效追踪器统计信息
func (t *CacheInvalidationTracker) GetStats() map[string]interface{} {
	t.mu.RLock()
	defer t.mu.RUnlock()

	return map[string]interface{}{
		"totalInvalidations": t.totalInvalidations,
		"reasonCount":        len(t.invalidations),
		"lastInvalidatedKey": t.lastInvalidatedKey,
		"lastReason":         t.lastReason,
	}
}

// Reset 重置追踪器
func (t *CacheInvalidationTracker) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.invalidations = make(map[string]int)
	t.totalInvalidations = 0
	t.lastInvalidatedKey = ""
	t.lastReason = ""
}

// citSortReasons 按失效次数降序排序原因，返回前 N 个原因字符串
// 同频时按原因字母序排列以保证稳定排序
func citSortReasons(reasons map[string]int, n int) []string {
	type reasonEntry struct {
		reason string
		count  int
	}

	list := make([]reasonEntry, 0, len(reasons))
	for reason, count := range reasons {
		list = append(list, reasonEntry{reason: reason, count: count})
	}

	// 按次数降序，同频按字母序
	for i := 0; i < len(list); i++ {
		for j := i + 1; j < len(list); j++ {
			if list[j].count > list[i].count ||
				(list[j].count == list[i].count && list[j].reason < list[i].reason) {
				list[i], list[j] = list[j], list[i]
			}
		}
	}

	if n < 0 {
		n = 0
	}
	if n > len(list) {
		n = len(list)
	}

	result := make([]string, n)
	for i := 0; i < n; i++ {
		result[i] = list[i].reason
	}
	return result
}

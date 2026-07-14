package agent

import "sync"

// ── OPT-150: CacheUtilizationTracker (缓存利用率追踪器) ──
// 追踪缓存空间利用率。记录插入和驱逐操作，维护利用率历史，
// 可查询当前利用率、利用率趋势（基于最近5个点）及综合统计。

// CacheUtilizationTracker 缓存利用率追踪器，追踪缓存空间利用率。
type CacheUtilizationTracker struct {
	mu                 sync.RWMutex
	totalEntries       int
	usedEntries        int
	totalCapacity      int
	totalInserts       int
	totalEvictions     int
	utilizationHistory []float64
	maxHistorySize     int
}

// NewCacheUtilizationTracker 创建一个新的缓存利用率追踪器。
// capacity 为缓存总容量，maxHistorySize 默认为 50。
func NewCacheUtilizationTracker(capacity int) *CacheUtilizationTracker {
	return &CacheUtilizationTracker{
		totalCapacity:      capacity,
		utilizationHistory: []float64{},
		maxHistorySize:     50,
	}
}

// RecordInsert 记录一次插入操作。
// usedEntries 递增（不超过 capacity），同时更新总计数和利用率历史。
func (t *CacheUtilizationTracker) RecordInsert() {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.totalInserts++
	t.totalEntries++
	if t.usedEntries < t.totalCapacity {
		t.usedEntries++
	}
	t.utilizationHistory = cutAppendUtilization(t.utilizationHistory, t.usedEntries, t.totalCapacity, t.maxHistorySize)
}

// RecordEviction 记录一次驱逐操作。
// usedEntries 递减（不低于 0），同时更新驱逐计数和利用率历史。
func (t *CacheUtilizationTracker) RecordEviction() {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.totalEvictions++
	if t.usedEntries > 0 {
		t.usedEntries--
	}
	t.utilizationHistory = cutAppendUtilization(t.utilizationHistory, t.usedEntries, t.totalCapacity, t.maxHistorySize)
}

// GetUtilization 返回当前缓存利用率（usedEntries / totalCapacity）。
func (t *CacheUtilizationTracker) GetUtilization() float64 {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if t.totalCapacity <= 0 {
		return 0.0
	}
	return float64(t.usedEntries) / float64(t.totalCapacity)
}

// GetUtilizationTrend 根据最近5个利用率历史点返回趋势。
// 返回 "increasing"、"decreasing" 或 "stable"。
func (t *CacheUtilizationTracker) GetUtilizationTrend() string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return cutComputeTrend(t.utilizationHistory)
}

// GetStats 返回追踪器的统计信息，包括 totalEntries、usedEntries、
// totalCapacity、utilization、totalInserts、totalEvictions 和 trend。
func (t *CacheUtilizationTracker) GetStats() map[string]interface{} {
	t.mu.RLock()
	defer t.mu.RUnlock()

	utilization := 0.0
	if t.totalCapacity > 0 {
		utilization = float64(t.usedEntries) / float64(t.totalCapacity)
	}

	return map[string]interface{}{
		"totalEntries":   t.totalEntries,
		"usedEntries":    t.usedEntries,
		"totalCapacity":  t.totalCapacity,
		"utilization":    utilization,
		"totalInserts":   t.totalInserts,
		"totalEvictions": t.totalEvictions,
		"trend":          cutComputeTrend(t.utilizationHistory),
	}
}

// Reset 重置追踪器的所有状态（不包括 totalCapacity 和 maxHistorySize 配置）。
func (t *CacheUtilizationTracker) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.totalEntries = 0
	t.usedEntries = 0
	t.totalInserts = 0
	t.totalEvictions = 0
	t.utilizationHistory = []float64{}
}

// cutAppendUtilization 计算当前利用率并追加到历史记录，超出 maxSize 时截断。
func cutAppendUtilization(history []float64, usedEntries, totalCapacity, maxSize int) []float64 {
	util := 0.0
	if totalCapacity > 0 {
		util = float64(usedEntries) / float64(totalCapacity)
	}
	history = append(history, util)
	if len(history) > maxSize {
		history = history[len(history)-maxSize:]
	}
	return history
}

// cutComputeTrend 根据利用率历史记录计算趋势。
// 取最近5个点，比较首尾差值，差值超过 0.05 视为上升或下降趋势。
func cutComputeTrend(history []float64) string {
	n := len(history)
	if n < 2 {
		return "stable"
	}
	start := n - 5
	if start < 0 {
		start = 0
	}
	recent := history[start:]
	first := recent[0]
	last := recent[len(recent)-1]
	diff := last - first
	threshold := 0.05
	if diff > threshold {
		return "increasing"
	}
	if diff < -threshold {
		return "decreasing"
	}
	return "stable"
}

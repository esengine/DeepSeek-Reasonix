package agent

import (
	"sort"
	"sync"
)

// ── OPT-86: 缓存失效追踪器 (Cache Invalidation Tracker) ──
// 追踪缓存失效事件及其成因，用于在未来减少缓存未命中。
//
// 原理：prompt 缓存一旦因前缀变化而失效，已缓存的 token 将全部作废。
// 通过记录每次失效的原因（例如：系统提示变更、工具描述轮换、消息插入、
// 前缀抖动等），可以定位最频繁的失效来源，进而针对性优化前缀稳定性。
//
// 效果：定位缓存失效的主要诱因后，可针对性修复，从而提升缓存命中率、
// 降低重复前缀的重复计费。

// CacheInvalidationTracker 追踪缓存失效事件及其成因
type CacheInvalidationTracker struct {
	mu                   sync.RWMutex
	totalInvalidations   int
	byCause              map[string]int
	tokensLost           int
	lastInvalidationTurn int
	invalidationHistory  []InvalidationRecord
}

// InvalidationRecord 单次缓存失效记录
type InvalidationRecord struct {
	Turn       int
	Cause      string
	TokensLost int
	PrefixHash string
}

// CauseCount 失效原因及其出现次数
type CauseCount struct {
	Cause string
	Count int
}

// CacheInvalidationStats 缓存失效统计
type CacheInvalidationStats struct {
	TotalInvalidations int
	TokensLost         int
	TopCauses          map[string]int
}

// NewCacheInvalidationTracker 创建缓存失效追踪器
func NewCacheInvalidationTracker() *CacheInvalidationTracker {
	return &CacheInvalidationTracker{
		byCause: make(map[string]int),
	}
}

// RecordInvalidation 记录一次缓存失效事件
func (t *CacheInvalidationTracker) RecordInvalidation(turn int, cause string, tokensLost int, prefixHash string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.totalInvalidations++
	t.byCause[cause]++
	t.tokensLost += tokensLost
	t.lastInvalidationTurn = turn
	t.invalidationHistory = append(t.invalidationHistory, InvalidationRecord{
		Turn:       turn,
		Cause:      cause,
		TokensLost: tokensLost,
		PrefixHash: prefixHash,
	})
}

// GetTopCauses 返回出现次数最多的前 N 个失效原因（按频率降序，同频按原因字母序）
func (t *CacheInvalidationTracker) GetTopCauses(n int) []CauseCount {
	t.mu.RLock()
	defer t.mu.RUnlock()

	list := make([]CauseCount, 0, len(t.byCause))
	for cause, count := range t.byCause {
		list = append(list, CauseCount{Cause: cause, Count: count})
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].Count != list[j].Count {
			return list[i].Count > list[j].Count
		}
		return list[i].Cause < list[j].Cause
	})

	if n < 0 {
		n = 0
	}
	if n > len(list) {
		n = len(list)
	}
	return list[:n]
}

// GetStats 返回缓存失效统计
func (t *CacheInvalidationTracker) GetStats() CacheInvalidationStats {
	t.mu.RLock()
	defer t.mu.RUnlock()

	topCauses := make(map[string]int, len(t.byCause))
	for k, v := range t.byCause {
		topCauses[k] = v
	}
	return CacheInvalidationStats{
		TotalInvalidations: t.totalInvalidations,
		TokensLost:         t.tokensLost,
		TopCauses:          topCauses,
	}
}

// Reset 重置追踪器
func (t *CacheInvalidationTracker) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.totalInvalidations = 0
	t.byCause = make(map[string]int)
	t.tokensLost = 0
	t.lastInvalidationTurn = 0
	t.invalidationHistory = nil
}

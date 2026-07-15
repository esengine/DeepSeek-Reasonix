package agent
import "sync"

// ── OPT-227: CacheInvalidationDeduplicator (缓存失效去重器) ──
// CacheInvalidationDeduplicator 避免重复的缓存失效操作，
// 对相同的失效 key 只放行一次。
type CacheInvalidationDeduplicator struct {
	mu                sync.RWMutex
	seen              map[string]bool
	deduplicatedCount int
	passedCount       int
}

// NewCacheInvalidationDeduplicator 创建缓存失效去重器。
func NewCacheInvalidationDeduplicator() *CacheInvalidationDeduplicator {
	return &CacheInvalidationDeduplicator{
		seen: make(map[string]bool),
	}
}

// Check 检查是否是新的失效请求。
// 返回 true 表示应执行失效操作，false 表示重复请求已被去重。
func (d *CacheInvalidationDeduplicator) Check(key string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.seen[key] {
		d.deduplicatedCount++
		return false
	}
	d.seen[key] = true
	d.passedCount++
	return true
}

// Reset 清除已见记录，允许 key 重新通过，并归零计数。
func (d *CacheInvalidationDeduplicator) Reset() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.seen = make(map[string]bool)
	d.deduplicatedCount = 0
	d.passedCount = 0
}

// GetDeduplicationRate 返回去重率（已去重数 / 总请求数）。
func (d *CacheInvalidationDeduplicator) GetDeduplicationRate() float64 {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return cidComputeDedupRate(d.deduplicatedCount, d.passedCount)
}

// GetStats 返回去重器的统计信息。
func (d *CacheInvalidationDeduplicator) GetStats() map[string]interface{} {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return map[string]interface{}{
		"seenCount":         len(d.seen),
		"deduplicatedCount": d.deduplicatedCount,
		"passedCount":       d.passedCount,
		"deduplicationRate": cidComputeDedupRate(d.deduplicatedCount, d.passedCount),
	}
}

// cidComputeDedupRate 计算去重率（已去重数 / 总请求数）。
// 当总请求数为 0 时返回 0。
func cidComputeDedupRate(deduplicatedCount, passedCount int) float64 {
	total := deduplicatedCount + passedCount
	if total == 0 {
		return 0
	}
	return float64(deduplicatedCount) / float64(total)
}

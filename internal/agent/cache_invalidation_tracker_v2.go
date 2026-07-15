package agent
import "sync"

// ── OPT-232: CacheInvalidationTrackerV2 (缓存失效追踪器V2 / Cache Invalidation Tracker V2) ──
// 追踪缓存失效的模式和频率，通过按 pattern 分类统计失效次数，
// 定位最频繁的失效模式，进而针对性优化缓存策略。
// 相比 V1 增加了模式追踪和按频率排序的能力。

// CacheInvalidationTrackerV2 缓存失效追踪器V2
type CacheInvalidationTrackerV2 struct {
	mu                  sync.RWMutex
	patterns            map[string]int // pattern -> 失效计数
	totalTracked        int            // 总追踪次数
	lastInvalidatedKey  string         // 最近失效的缓存 key
	trackingStartTime   int64          // 追踪开始时间戳
}

// NewCacheInvalidationTrackerV2 创建一个新的缓存失效追踪器V2。
func NewCacheInvalidationTrackerV2() *CacheInvalidationTrackerV2 {
	return &CacheInvalidationTrackerV2{
		patterns: make(map[string]int),
	}
}

// Track 追踪一次缓存失效事件。
// key 为被失效的缓存键，pattern 为失效模式（如 "ttl_expire"、"manual"、"size_evict"）。
// 首次追踪时记录开始时间标记。
func (t *CacheInvalidationTrackerV2) Track(key string, pattern string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	// 首次追踪时标记开始
	if t.trackingStartTime == 0 {
		t.trackingStartTime = 1
	}

	t.patterns[pattern]++
	t.totalTracked++
	t.lastInvalidatedKey = key
}

// GetPatternCount 获取指定模式的失效计数。
func (t *CacheInvalidationTrackerV2) GetPatternCount(pattern string) int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.patterns[pattern]
}

// GetTopPatterns 获取出现次数最多的 n 个模式（按计数降序，同频按字母序排列）。
func (t *CacheInvalidationTrackerV2) GetTopPatterns(n int) []string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return citv2SortPatterns(t.patterns, n)
}

// GetStats 返回追踪器的统计信息。
// 包含 patternCount、totalTracked、lastInvalidatedKey、trackingStartTime。
func (t *CacheInvalidationTrackerV2) GetStats() map[string]interface{} {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return map[string]interface{}{
		"patternCount":       len(t.patterns),
		"totalTracked":       t.totalTracked,
		"lastInvalidatedKey": t.lastInvalidatedKey,
		"trackingStartTime":  t.trackingStartTime,
	}
}

// Reset 重置追踪器状态（清空模式统计、计数和最近失效键）。
func (t *CacheInvalidationTrackerV2) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.patterns = make(map[string]int)
	t.totalTracked = 0
	t.lastInvalidatedKey = ""
	t.trackingStartTime = 0
}

// citv2SortPatterns 按失效计数降序排序模式，返回前 n 个模式字符串（辅助函数）。
// 同频时按模式字母序排列以保证稳定排序。
func citv2SortPatterns(patterns map[string]int, n int) []string {
	type patternEntry struct {
		pattern string
		count   int
	}

	list := make([]patternEntry, 0, len(patterns))
	for pattern, count := range patterns {
		list = append(list, patternEntry{pattern: pattern, count: count})
	}

	// 按计数降序，同频按字母序
	for i := 0; i < len(list); i++ {
		for j := i + 1; j < len(list); j++ {
			if list[j].count > list[i].count ||
				(list[j].count == list[i].count && list[j].pattern < list[i].pattern) {
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
		result[i] = list[i].pattern
	}
	return result
}

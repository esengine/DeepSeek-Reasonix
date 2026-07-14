package agent

import "sync"

// ── OPT-124: CacheHitAnalyzer (缓存命中分析器) ──
// 分析缓存命中模式，跟踪命中率、命中/未命中的模式分布，
// 并识别最常命中的缓存模式，为缓存策略优化提供数据支撑。
//
// 核心能力：
//   - 记录每次缓存命中/未命中及其关联模式
//   - 实时计算命中率
//   - 识别最常命中的缓存模式
//   - 维护最近请求结果序列供趋势分析

// CacheHitAnalyzer 缓存命中分析器，分析缓存命中模式。
type CacheHitAnalyzer struct {
	mu            sync.RWMutex
	totalRequests int
	totalHits     int
	totalMisses   int
	hitPatterns   map[string]int
	missPatterns  map[string]int
	recentResults []bool
}

// NewCacheHitAnalyzer 创建一个新的缓存命中分析器实例。
func NewCacheHitAnalyzer() *CacheHitAnalyzer {
	return &CacheHitAnalyzer{
		hitPatterns:   make(map[string]int),
		missPatterns:  make(map[string]int),
		recentResults: make([]bool, 0),
	}
}

// RecordHit 记录一次缓存命中及其关联模式。
func (a *CacheHitAnalyzer) RecordHit(pattern string) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.totalRequests++
	a.totalHits++
	a.hitPatterns[pattern]++
	a.recentResults = append(a.recentResults, true)
}

// RecordMiss 记录一次缓存未命中及其关联模式。
func (a *CacheHitAnalyzer) RecordMiss(pattern string) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.totalRequests++
	a.totalMisses++
	a.missPatterns[pattern]++
	a.recentResults = append(a.recentResults, false)
}

// GetHitRate 返回缓存命中率（0-1）。若无请求记录则返回 0。
func (a *CacheHitAnalyzer) GetHitRate() float64 {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.totalRequests == 0 {
		return 0
	}
	return float64(a.totalHits) / float64(a.totalRequests)
}

// GetTopHitPattern 返回最常命中的缓存模式。若无命中记录，返回空字符串。
func (a *CacheHitAnalyzer) GetTopHitPattern() string {
	a.mu.RLock()
	defer a.mu.RUnlock()

	return chaFindTopPattern(a.hitPatterns)
}

// GetStats 返回分析器的统计信息，包括总请求数、命中数、
// 未命中数、命中率、最常命中模式和模式总数。
func (a *CacheHitAnalyzer) GetStats() map[string]interface{} {
	a.mu.RLock()
	defer a.mu.RUnlock()

	stats := make(map[string]interface{})
	stats["totalRequests"] = a.totalRequests
	stats["totalHits"] = a.totalHits
	stats["totalMisses"] = a.totalMisses

	hitRate := 0.0
	if a.totalRequests > 0 {
		hitRate = float64(a.totalHits) / float64(a.totalRequests)
	}
	stats["hitRate"] = hitRate
	stats["topHitPattern"] = chaFindTopPattern(a.hitPatterns)
	stats["patternCount"] = len(a.hitPatterns) + len(a.missPatterns)

	return stats
}

// Reset 重置分析器的所有统计数据。
func (a *CacheHitAnalyzer) Reset() {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.totalRequests = 0
	a.totalHits = 0
	a.totalMisses = 0
	a.hitPatterns = make(map[string]int)
	a.missPatterns = make(map[string]int)
	a.recentResults = make([]bool, 0)
}

// chaFindTopPattern 在模式映射中查找出现次数最多的模式。
func chaFindTopPattern(patterns map[string]int) string {
	topPattern := ""
	topCount := 0
	for pattern, count := range patterns {
		if count > topCount {
			topCount = count
			topPattern = pattern
		}
	}
	return topPattern
}

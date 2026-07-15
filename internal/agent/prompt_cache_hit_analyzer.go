package agent

import "sync"

// ── OPT-215: PromptCacheHitAnalyzer (提示缓存命中分析器) ──
// 分析缓存命中模式以优化策略。通过记录每个模式的命中/未命中历史，
// 实时计算命中率和最佳模式，为缓存策略优化提供数据支撑。
//
// 核心能力：
//   - RecordHit: 记录指定模式的命中或未命中
//   - GetHitRate: 获取指定模式的命中率
//   - GetOverallHitRate: 获取全局命中率
//   - GetBestPattern: 获取命中率最高的模式

// PromptCacheHitAnalyzer 提示缓存命中分析器。
type PromptCacheHitAnalyzer struct {
	mu          sync.RWMutex
	hitHistory  map[string][]bool // pattern → hit history
	totalHits   int
	totalMisses int
	patterns    int
}

// NewPromptCacheHitAnalyzer 创建一个新的提示缓存命中分析器实例。
func NewPromptCacheHitAnalyzer() *PromptCacheHitAnalyzer {
	return &PromptCacheHitAnalyzer{
		hitHistory: make(map[string][]bool),
	}
}

// RecordHit 记录一次缓存命中或未命中。
// pattern标识缓存模式，hit为true表示命中，false表示未命中。
// 首次记录某pattern时递增patterns计数。
func (a *PromptCacheHitAnalyzer) RecordHit(pattern string, hit bool) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if _, exists := a.hitHistory[pattern]; !exists {
		a.patterns++
	}
	a.hitHistory[pattern] = append(a.hitHistory[pattern], hit)
	if hit {
		a.totalHits++
	} else {
		a.totalMisses++
	}
}

// GetHitRate 获取指定模式的命中率（0-1）。
// 若该模式无记录返回0。
func (a *PromptCacheHitAnalyzer) GetHitRate(pattern string) float64 {
	a.mu.RLock()
	defer a.mu.RUnlock()

	return pchaComputeHitRate(a.hitHistory[pattern])
}

// GetOverallHitRate 获取全局命中率（0-1）。
// 若无任何记录返回0。
func (a *PromptCacheHitAnalyzer) GetOverallHitRate() float64 {
	a.mu.RLock()
	defer a.mu.RUnlock()

	total := a.totalHits + a.totalMisses
	if total == 0 {
		return 0
	}
	return float64(a.totalHits) / float64(total)
}

// GetBestPattern 获取命中率最高的模式。
// 若无任何模式记录返回空字符串。
func (a *PromptCacheHitAnalyzer) GetBestPattern() string {
	a.mu.RLock()
	defer a.mu.RUnlock()

	bestPattern := ""
	bestRate := -1.0
	for pattern, history := range a.hitHistory {
		rate := pchaComputeHitRate(history)
		if rate > bestRate {
			bestRate = rate
			bestPattern = pattern
		}
	}
	return bestPattern
}

// GetStats 返回分析器的统计信息。
func (a *PromptCacheHitAnalyzer) GetStats() map[string]interface{} {
	a.mu.RLock()
	defer a.mu.RUnlock()

	total := a.totalHits + a.totalMisses
	overallHitRate := 0.0
	if total > 0 {
		overallHitRate = float64(a.totalHits) / float64(total)
	}
	return map[string]interface{}{
		"patterns":       a.patterns,
		"totalHits":      a.totalHits,
		"totalMisses":    a.totalMisses,
		"overallHitRate": overallHitRate,
	}
}

// Reset 重置分析器为初始状态。
func (a *PromptCacheHitAnalyzer) Reset() {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.hitHistory = make(map[string][]bool)
	a.totalHits = 0
	a.totalMisses = 0
	a.patterns = 0
}

// pchaComputeHitRate 根据命中历史计算命中率。
// 命中率 = 命中次数 / 总记录数。若历史为空返回0。
func pchaComputeHitRate(history []bool) float64 {
	if len(history) == 0 {
		return 0
	}
	hits := 0
	for _, h := range history {
		if h {
			hits++
		}
	}
	return float64(hits) / float64(len(history))
}

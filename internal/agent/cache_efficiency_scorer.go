package agent

import "sync"

// ── OPT-131: CacheEfficiencyScorer (缓存效率评分器) ──
// 评估缓存系统的整体效率。通过命中率、节省比率与开销比率的加权组合
// 计算综合评分 (0-1)，并维护历史记录以支持趋势分析。
//
// 评分公式: hitRate * 0.4 + savingsRatio * 0.4 - overheadRatio * 0.2
// 其中:
//   - hitRate = hits / (hits + misses)
//   - savingsRatio = totalSaved / (totalSaved + totalOverhead)
//   - overheadRatio = totalOverhead / (totalSaved + totalOverhead)

// CacheEfficiencyScorer 缓存效率评分器，评估缓存系统的整体效率。
type CacheEfficiencyScorer struct {
	mu             sync.RWMutex
	totalScores    int
	totalScore     float64
	bestScore      float64
	worstScore     float64
	scoreHistory   []float64
	maxHistorySize int
}

// NewCacheEfficiencyScorer 创建一个新的缓存效率评分器实例。
// maxHistorySize 默认设置为 50。
func NewCacheEfficiencyScorer() *CacheEfficiencyScorer {
	return &CacheEfficiencyScorer{
		maxHistorySize: 50,
	}
}

// Score 根据缓存命中、未命中、节省 token 与开销 token 计算综合效率评分。
// 评分范围为 0-1，公式为:
//
//	hitRate*0.4 + savingsRatio*0.4 - overheadRatio*0.2
//
// 并更新内部统计数据与历史记录。
func (s *CacheEfficiencyScorer) Score(hits int, misses int, totalSaved int, totalOverhead int) float64 {
	s.mu.Lock()
	defer s.mu.Unlock()

	hitRate := cesCalcHitRate(hits, misses)
	savingsRatio := cesCalcSavingsRatio(totalSaved, totalOverhead)
	overheadRatio := cesCalcOverheadRatio(totalSaved, totalOverhead)

	score := hitRate*0.4 + savingsRatio*0.4 - overheadRatio*0.2
	score = cesClamp(score, 0, 1)

	s.totalScores++
	s.totalScore += score
	if s.totalScores == 1 {
		s.bestScore = score
		s.worstScore = score
	} else {
		if score > s.bestScore {
			s.bestScore = score
		}
		if score < s.worstScore {
			s.worstScore = score
		}
	}

	s.scoreHistory = append(s.scoreHistory, score)
	if len(s.scoreHistory) > s.maxHistorySize {
		s.scoreHistory = s.scoreHistory[len(s.scoreHistory)-s.maxHistorySize:]
	}

	return score
}

// GetScoreCategory 根据评分返回类别标签。
// 评分区间: <0.3 "poor", <0.5 "fair", <0.7 "good", <0.9 "excellent", 其余 "optimal"。
func (s *CacheEfficiencyScorer) GetScoreCategory(score float64) string {
	return cesCategory(score)
}

// GetStats 返回缓存效率评分器的统计信息。
func (s *CacheEfficiencyScorer) GetStats() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	avgScore := 0.0
	if s.totalScores > 0 {
		avgScore = s.totalScore / float64(s.totalScores)
	}

	lastCategory := "none"
	if len(s.scoreHistory) > 0 {
		lastCategory = cesCategory(s.scoreHistory[len(s.scoreHistory)-1])
	}

	return map[string]interface{}{
		"totalScores":  s.totalScores,
		"avgScore":     avgScore,
		"bestScore":    s.bestScore,
		"worstScore":   s.worstScore,
		"lastCategory": lastCategory,
		"historySize":  len(s.scoreHistory),
	}
}

// Reset 重置评分器的所有统计数据和历史记录。
func (s *CacheEfficiencyScorer) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.totalScores = 0
	s.totalScore = 0
	s.bestScore = 0
	s.worstScore = 0
	s.scoreHistory = nil
}

// ---------------------------------------------------------------------------
// 辅助函数 (ces 前缀)
// ---------------------------------------------------------------------------

// cesCalcHitRate 计算缓存命中率: hits / (hits + misses)。
// 若 hits + misses 为 0，返回 0。
func cesCalcHitRate(hits, misses int) float64 {
	total := hits + misses
	if total == 0 {
		return 0
	}
	return float64(hits) / float64(total)
}

// cesCalcSavingsRatio 计算节省比率: totalSaved / (totalSaved + totalOverhead)。
// 若分母为 0，返回 0。
func cesCalcSavingsRatio(totalSaved, totalOverhead int) float64 {
	total := totalSaved + totalOverhead
	if total == 0 {
		return 0
	}
	return float64(totalSaved) / float64(total)
}

// cesCalcOverheadRatio 计算开销比率: totalOverhead / (totalSaved + totalOverhead)。
// 若分母为 0，返回 0。
func cesCalcOverheadRatio(totalSaved, totalOverhead int) float64 {
	total := totalSaved + totalOverhead
	if total == 0 {
		return 0
	}
	return float64(totalOverhead) / float64(total)
}

// cesClamp 将值限制在 [min, max] 范围内。
func cesClamp(value, min, max float64) float64 {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

// cesCategory 将评分映射为类别字符串。
func cesCategory(score float64) string {
	switch {
	case score < 0.3:
		return "poor"
	case score < 0.5:
		return "fair"
	case score < 0.7:
		return "good"
	case score < 0.9:
		return "excellent"
	default:
		return "optimal"
	}
}

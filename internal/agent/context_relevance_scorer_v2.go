package agent

import "sync"

// ── OPT-223: ContextRelevanceScorerV2 (上下文相关性评分器 V2) ──
// 评估上下文片段与查询的相关性，基于词重叠率（Jaccard）计算 0~1 的分数，
// 并按阈值判定是否相关。评分按片段缓存，支持历史查询与统计。
//
// 注意：本模块为 OPT-223，与已有的 OPT-153 ContextRelevanceScorer 功能相近
// 但设计不同（增加 threshold 阈值与按片段缓存 scores）。由于 Go 不支持同包
// 内的类型/构造函数重载，为避免与 OPT-153 的 ContextRelevanceScorer /
// NewContextRelevanceScorer 命名冲突，本模块采用 V2 命名后缀（与本仓库
// _v2 后继模块的既有约定一致）。
//
// 原理：将片段与查询按空白分词并转小写，计算 |交集| / |并集| 作为相关性
// 分数；分数严格大于 threshold 即判定为相关。
//
// 效果：为上下文裁剪、优先级排序提供量化依据，并保留评分历史以支持
// 趋势分析与阈值过滤。

// ContextRelevanceScorerV2 上下文相关性评分器（OPT-223）。
type ContextRelevanceScorerV2 struct {
	mu          sync.RWMutex
	scores      map[string]float64 // fragment → score
	totalScored int
	avgScore    float64
	threshold   float64
}

// NewContextRelevanceScorerV2 创建评分器，threshold 会被限制在 [0,1] 区间。
func NewContextRelevanceScorerV2(threshold float64) *ContextRelevanceScorerV2 {
	if threshold < 0 {
		threshold = 0
	}
	if threshold > 1 {
		threshold = 1
	}
	return &ContextRelevanceScorerV2{
		scores:    make(map[string]float64),
		threshold: threshold,
	}
}

// Score 计算片段与查询的相关性分数 (0~1)，基于词重叠率。
// 同时缓存该片段的分数并增量更新平均分。
func (s *ContextRelevanceScorerV2) Score(fragment string, query string) float64 {
	score := crsComputeOverlap(fragment, query)

	s.mu.Lock()
	defer s.mu.Unlock()

	s.scores[fragment] = score
	s.totalScored++
	if s.totalScored == 1 {
		s.avgScore = score
	} else {
		s.avgScore = s.avgScore + (score-s.avgScore)/float64(s.totalScored)
	}
	return score
}

// IsRelevant 判定片段与查询是否相关（分数严格大于阈值）。
// 内部会调用 Score 记录评分后再与阈值比较。
func (s *ContextRelevanceScorerV2) IsRelevant(fragment string, query string) bool {
	score := s.Score(fragment, query)

	s.mu.RLock()
	defer s.mu.RUnlock()

	return score > s.threshold
}

// GetScore 返回已记录的片段分数；若未评分则返回 0。
func (s *ContextRelevanceScorerV2) GetScore(fragment string) float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.scores[fragment]
}

// GetStats 返回统计信息：fragmentCount、totalScored、avgScore、threshold。
func (s *ContextRelevanceScorerV2) GetStats() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return map[string]interface{}{
		"fragmentCount": len(s.scores),
		"totalScored":   s.totalScored,
		"avgScore":      s.avgScore,
		"threshold":     s.threshold,
	}
}

// Reset 重置评分器，清除评分缓存与统计（保留阈值）。
func (s *ContextRelevanceScorerV2) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.scores = make(map[string]float64)
	s.totalScored = 0
	s.avgScore = 0
}

// ---------------------------------------------------------------------------
// 辅助函数（以 crs 为前缀，避免命名冲突）
// ---------------------------------------------------------------------------

// crsComputeOverlap 计算片段与查询的词重叠率（Jaccard 相似度）。
// 将两者按空白分词并转小写，返回 |交集| / |并集|；两者均为空时返回 0。
func crsComputeOverlap(fragment, query string) float64 {
	tokenize := func(s string) map[string]bool {
		tokens := make(map[string]bool)
		var word []byte
		for i := 0; i < len(s); i++ {
			ch := s[i]
			if ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r' {
				if len(word) > 0 {
					tokens[string(word)] = true
					word = word[:0]
				}
			} else {
				if ch >= 'A' && ch <= 'Z' {
					ch += 32
				}
				word = append(word, ch)
			}
		}
		if len(word) > 0 {
			tokens[string(word)] = true
		}
		return tokens
	}

	ft := tokenize(fragment)
	qt := tokenize(query)

	if len(ft) == 0 && len(qt) == 0 {
		return 0
	}

	intersection := 0
	for k := range qt {
		if ft[k] {
			intersection++
		}
	}

	union := len(ft) + len(qt) - intersection
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}

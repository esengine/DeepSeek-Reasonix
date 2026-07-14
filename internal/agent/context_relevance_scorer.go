package agent

import "sync"

// ── OPT-153: ContextRelevanceScorer (上下文相关性评分器) ──
// 对上下文片段按与当前查询的相关性评分。使用 Jaccard 相似度计算
// 片段与查询的词重叠率，分数范围 0~1：0 表示完全无关，1 表示完全相关。
//
// 原理：在上下文窗口有限的情况下，需要优先保留与当前查询最相关的片段。
// Jaccard 相似度 = |A ∩ B| / |A ∪ B|，其中 A 和 B 分别为片段与查询的
// 词集合（按空格分词并转小写）。该指标计算简单高效，适合实时评分场景。
//
// 效果：为上下文裁剪与优先级排序提供量化依据，帮助在有限窗口内
// 保留最相关的信息，同时维护评分历史以支持趋势分析。

// ContextRelevanceScorer 上下文相关性评分器
type ContextRelevanceScorer struct {
	mu           sync.RWMutex
	scoredSegments int
	avgScore     float64
	scoreHistory []float64
	maxHistory   int
}

// NewContextRelevanceScorer 创建上下文相关性评分器。
// maxHistory 默认设置为 100。
func NewContextRelevanceScorer() *ContextRelevanceScorer {
	return &ContextRelevanceScorer{
		maxHistory: 100,
	}
}

// Score 基于词重叠率计算片段与查询的相关性分数 (0~1)。
// 使用 Jaccard 相似度：|segment ∩ query| / |segment ∪ query|。
// 每次调用递增 scoredSegments，并更新 scoreHistory 与 avgScore。
func (s *ContextRelevanceScorer) Score(segment string, query string) float64 {
	s.mu.Lock()
	defer s.mu.Unlock()

	segTokens := crsTokenize(segment)
	queryTokens := crsTokenize(query)
	score := crsJaccard(segTokens, queryTokens)

	s.scoredSegments++
	s.scoreHistory = append(s.scoreHistory, score)
	if len(s.scoreHistory) > s.maxHistory {
		s.scoreHistory = s.scoreHistory[len(s.scoreHistory)-s.maxHistory:]
	}
	s.avgScore = crsComputeAvg(s.scoreHistory)

	return score
}

// ScoreBatch 批量评分，对每个片段调用 Score 并返回分数列表。
func (s *ContextRelevanceScorer) ScoreBatch(segments []string, query string) []float64 {
	scores := make([]float64, len(segments))
	for i, seg := range segments {
		scores[i] = s.Score(seg, query)
	}
	return scores
}

// GetTopSegments 返回得分最高的 N 个片段的索引（按得分降序排列）。
// 若 topN 大于片段数，则返回所有片段的索引。
func (s *ContextRelevanceScorer) GetTopSegments(segments []string, query string, topN int) []int {
	scores := s.ScoreBatch(segments, query)

	if topN <= 0 || len(segments) == 0 {
		return []int{}
	}

	if topN > len(segments) {
		topN = len(segments)
	}

	// 使用选择排序找到 topN 个最高分的索引
	type idxScore struct {
		idx   int
		score float64
	}
	pairs := make([]idxScore, len(scores))
	for i, sc := range scores {
		pairs[i] = idxScore{idx: i, score: sc}
	}

	result := make([]int, 0, topN)
	for i := 0; i < topN; i++ {
		maxIdx := i
		for j := i + 1; j < len(pairs); j++ {
			if pairs[j].score > pairs[maxIdx].score {
				maxIdx = j
			}
		}
		pairs[i], pairs[maxIdx] = pairs[maxIdx], pairs[i]
		result = append(result, pairs[i].idx)
	}

	return result
}

// GetStats 返回评分器的统计信息，包括 scoredSegments、avgScore、lastScore。
func (s *ContextRelevanceScorer) GetStats() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	lastScore := 0.0
	if len(s.scoreHistory) > 0 {
		lastScore = s.scoreHistory[len(s.scoreHistory)-1]
	}

	return map[string]interface{}{
		"scoredSegments": s.scoredSegments,
		"avgScore":       s.avgScore,
		"lastScore":      lastScore,
	}
}

// Reset 重置评分器，清除所有统计数据与历史记录。
func (s *ContextRelevanceScorer) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.scoredSegments = 0
	s.avgScore = 0
	s.scoreHistory = nil
}

// ---------------------------------------------------------------------------
// 辅助函数（以 crs 为前缀，避免命名冲突）
// ---------------------------------------------------------------------------

// crsTokenize 将字符串按空格分词并转小写，返回词集合（map[string]bool）。
// 连续的空白字符（空格、制表符、换行符、回车）作为分隔符。
func crsTokenize(s string) map[string]bool {
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
			// ASCII 大写转小写
			if ch >= 'A' && ch <= 'Z' {
				ch = ch + 32
			}
			word = append(word, ch)
		}
	}

	if len(word) > 0 {
		tokens[string(word)] = true
	}

	return tokens
}

// crsJaccard 计算两个词集合的 Jaccard 相似度: |A ∩ B| / |A ∪ B|。
// 若两个集合均为空，返回 0。
func crsJaccard(setA, setB map[string]bool) float64 {
	if len(setA) == 0 && len(setB) == 0 {
		return 0
	}

	intersection := 0
	for k := range setA {
		if setB[k] {
			intersection++
		}
	}

	union := len(setA) + len(setB) - intersection
	if union == 0 {
		return 0
	}

	return float64(intersection) / float64(union)
}

// crsComputeAvg 计算分数列表的平均值。
// 若列表为空，返回 0。
func crsComputeAvg(history []float64) float64 {
	if len(history) == 0 {
		return 0
	}

	sum := 0.0
	for _, v := range history {
		sum += v
	}
	return sum / float64(len(history))
}

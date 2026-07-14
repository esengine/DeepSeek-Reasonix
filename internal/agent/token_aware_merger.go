package agent

import "sync"

// ── OPT-149: TokenAwareMerger (Token 感知合并器) ──
// 合并相似消息以减少 token 消耗。基于 Jaccard 相似度判断两消息是否可合并，
// 合并时取并集 token 并保持首次出现顺序去重。Token 估算使用字符数 len(s)。

// TokenAwareMerger Token 感知合并器，合并相似消息减少 token 消耗。
type TokenAwareMerger struct {
	mu               sync.RWMutex
	totalMerged      int
	totalTokensSaved int
	mergeThreshold   float64
	totalAttempts    int
}

// NewTokenAwareMerger 创建一个新的 Token 感知合并器。
// threshold 为合并的相似度阈值（0-1），两消息 Jaccard 相似度 >= threshold 时可合并。
func NewTokenAwareMerger(threshold float64) *TokenAwareMerger {
	return &TokenAwareMerger{
		mergeThreshold: threshold,
	}
}

// CanMerge 判断两消息是否可合并。
// 当两消息的 Jaccard 相似度 >= 合并阈值时返回 true。
func (m *TokenAwareMerger) CanMerge(msg1 string, msg2 string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	similarity := tam2JaccardSimilarity(tam2Tokenize(msg1), tam2Tokenize(msg2))
	return similarity >= m.mergeThreshold
}

// Merge 合并两消息：取并集 token，保持首次出现顺序并去重。
// 同时更新合并统计（totalMerged、totalAttempts、totalTokensSaved）。
func (m *TokenAwareMerger) Merge(msg1 string, msg2 string) string {
	m.mu.Lock()
	defer m.mu.Unlock()

	result := tam2DoMerge(msg1, msg2)
	saved := len(msg1) + len(msg2) - len(result)
	if saved < 0 {
		saved = 0
	}
	m.totalMerged++
	m.totalAttempts++
	m.totalTokensSaved += saved
	return result
}

// BatchMerge 批量合并相邻可合并的消息。
// 遍历消息列表，对相邻消息对尝试合并，可合并则合并并更新统计。
// 返回合并后的消息列表。
func (m *TokenAwareMerger) BatchMerge(messages []string) []string {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(messages) == 0 {
		return []string{}
	}

	result := []string{messages[0]}
	for i := 1; i < len(messages); i++ {
		m.totalAttempts++
		last := result[len(result)-1]
		similarity := tam2JaccardSimilarity(tam2Tokenize(last), tam2Tokenize(messages[i]))
		if similarity >= m.mergeThreshold {
			merged := tam2DoMerge(last, messages[i])
			saved := len(last) + len(messages[i]) - len(merged)
			if saved < 0 {
				saved = 0
			}
			result[len(result)-1] = merged
			m.totalMerged++
			m.totalTokensSaved += saved
		} else {
			result = append(result, messages[i])
		}
	}
	return result
}

// GetStats 返回合并器的统计信息，包括 totalMerged、totalTokensSaved、
// totalAttempts、mergeRate 和 avgTokensSaved。
func (m *TokenAwareMerger) GetStats() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	mergeRate := 0.0
	if m.totalAttempts > 0 {
		mergeRate = float64(m.totalMerged) / float64(m.totalAttempts)
	}
	avgTokensSaved := 0.0
	if m.totalMerged > 0 {
		avgTokensSaved = float64(m.totalTokensSaved) / float64(m.totalMerged)
	}

	return map[string]interface{}{
		"totalMerged":      m.totalMerged,
		"totalTokensSaved": m.totalTokensSaved,
		"totalAttempts":    m.totalAttempts,
		"mergeRate":        mergeRate,
		"avgTokensSaved":   avgTokensSaved,
	}
}

// Reset 重置合并器的统计计数（不影响合并阈值配置）。
func (m *TokenAwareMerger) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.totalMerged = 0
	m.totalTokensSaved = 0
	m.totalAttempts = 0
}

// tam2Tokenize 将字符串按空白字符分割为 token 列表。
func tam2Tokenize(s string) []string {
	var tokens []string
	var current []rune
	for _, r := range s {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			if len(current) > 0 {
				tokens = append(tokens, string(current))
				current = current[:0]
			}
		} else {
			current = append(current, r)
		}
	}
	if len(current) > 0 {
		tokens = append(tokens, string(current))
	}
	return tokens
}

// tam2JoinTokens 用空格连接 token 列表为字符串。
func tam2JoinTokens(tokens []string) string {
	if len(tokens) == 0 {
		return ""
	}
	result := tokens[0]
	for i := 1; i < len(tokens); i++ {
		result += " " + tokens[i]
	}
	return result
}

// tam2JaccardSimilarity 计算两组 token 的 Jaccard 相似度。
// Jaccard = |交集| / |并集|。两组均为空时返回 0。
func tam2JaccardSimilarity(tokens1, tokens2 []string) float64 {
	if len(tokens1) == 0 && len(tokens2) == 0 {
		return 0.0
	}
	set1 := make(map[string]struct{}, len(tokens1))
	for _, t := range tokens1 {
		set1[t] = struct{}{}
	}
	set2 := make(map[string]struct{}, len(tokens2))
	for _, t := range tokens2 {
		set2[t] = struct{}{}
	}

	intersection := 0
	for t := range set1 {
		if _, ok := set2[t]; ok {
			intersection++
		}
	}

	union := len(set1) + len(set2) - intersection
	if union == 0 {
		return 0.0
	}
	return float64(intersection) / float64(union)
}

// tam2MergeTokens 合并两组 token，取并集并保持首次出现顺序去重。
func tam2MergeTokens(tokens1, tokens2 []string) []string {
	seen := make(map[string]struct{}, len(tokens1)+len(tokens2))
	merged := make([]string, 0, len(tokens1)+len(tokens2))
	for _, t := range tokens1 {
		if _, ok := seen[t]; !ok {
			seen[t] = struct{}{}
			merged = append(merged, t)
		}
	}
	for _, t := range tokens2 {
		if _, ok := seen[t]; !ok {
			seen[t] = struct{}{}
			merged = append(merged, t)
		}
	}
	return merged
}

// tam2DoMerge 执行实际的消息合并（不更新统计）。
func tam2DoMerge(msg1, msg2 string) string {
	merged := tam2MergeTokens(tam2Tokenize(msg1), tam2Tokenize(msg2))
	return tam2JoinTokens(merged)
}

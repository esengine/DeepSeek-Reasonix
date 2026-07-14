package agent

import "sync"

// ── OPT-138: ConversationFlowAnalyzer (对话流分析器) ──
// 分析对话的流畅度与连贯性。通过计算相邻消息间的 Jaccard 词集相似度
// 得到平均相似度，再减去话题断裂数占消息数的比例，得到流分数 (0-1)。
//
// 流类别:
//   - <0.3 "disjointed"
//   - <0.6 "interrupted"
//   - <0.8 "smooth"
//   - 其余 "seamless"
//
// 流断裂判定: 相邻消息 Jaccard 相似度 < 0.2。

// ConversationFlowAnalyzer 对话流分析器，分析对话的流畅度和连贯性。
type ConversationFlowAnalyzer struct {
	mu             sync.RWMutex
	totalAnalyses  int
	avgFlowScore   float64
	totalBreaks    int
	flowHistory    []float64
	maxHistorySize int
}

// NewConversationFlowAnalyzer 创建一个新的对话流分析器。
// maxHistorySize 默认设置为 30。
func NewConversationFlowAnalyzer() *ConversationFlowAnalyzer {
	return &ConversationFlowAnalyzer{
		maxHistorySize: 30,
		flowHistory:    make([]float64, 0),
	}
}

// AnalyzeFlow 分析一组消息的对话流分数 (0-1)。
// 分数 = 相邻消息平均 Jaccard 相似度 - (话题断裂数 / 消息数)，
// 结果限制在 [0,1] 范围内。同时更新内部统计与历史记录。
func (a *ConversationFlowAnalyzer) AnalyzeFlow(messages []string) float64 {
	a.mu.Lock()
	defer a.mu.Unlock()

	n := len(messages)
	if n == 0 {
		return 0
	}

	sumSim := 0.0
	breaks := 0
	pairs := 0
	for i := 1; i < n; i++ {
		prev := cfaTokenize(messages[i-1])
		curr := cfaTokenize(messages[i])
		sim := cfaJaccard(prev, curr)
		sumSim += sim
		pairs++
		if cfaIsBreak(sim) {
			breaks++
		}
	}

	avgSim := 0.0
	if pairs > 0 {
		avgSim = sumSim / float64(pairs)
	}
	breakRatio := float64(breaks) / float64(n)
	score := cfaClamp(avgSim-breakRatio, 0, 1)

	a.totalAnalyses++
	a.totalBreaks += breaks
	a.avgFlowScore = cfaUpdateAvg(a.avgFlowScore, a.totalAnalyses, score)

	a.flowHistory = append(a.flowHistory, score)
	if len(a.flowHistory) > a.maxHistorySize {
		a.flowHistory = a.flowHistory[len(a.flowHistory)-a.maxHistorySize:]
	}
	return score
}

// DetectFlowBreak 检测两条相邻消息之间是否存在流断裂。
// 当 Jaccard 相似度 < 0.2 时判定为断裂。
func (a *ConversationFlowAnalyzer) DetectFlowBreak(prevMsg string, currMsg string) bool {
	sim := cfaJaccard(cfaTokenize(prevMsg), cfaTokenize(currMsg))
	return cfaIsBreak(sim)
}

// GetFlowCategory 根据流分数返回类别标签。
// 评分区间: <0.3 "disjointed", <0.6 "interrupted", <0.8 "smooth", 其余 "seamless"。
func (a *ConversationFlowAnalyzer) GetFlowCategory(score float64) string {
	return cfaCategory(score)
}

// GetStats 返回对话流分析器的统计信息。
// 包含 totalAnalyses、avgFlowScore、totalBreaks、lastCategory、historySize。
func (a *ConversationFlowAnalyzer) GetStats() map[string]interface{} {
	a.mu.RLock()
	defer a.mu.RUnlock()

	lastCategory := "none"
	if len(a.flowHistory) > 0 {
		lastCategory = cfaCategory(a.flowHistory[len(a.flowHistory)-1])
	}
	return map[string]interface{}{
		"totalAnalyses": a.totalAnalyses,
		"avgFlowScore":  a.avgFlowScore,
		"totalBreaks":   a.totalBreaks,
		"lastCategory":  lastCategory,
		"historySize":   len(a.flowHistory),
	}
}

// Reset 重置分析器的所有统计数据和历史记录。
func (a *ConversationFlowAnalyzer) Reset() {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.totalAnalyses = 0
	a.avgFlowScore = 0
	a.totalBreaks = 0
	a.flowHistory = nil
}

// ---------------------------------------------------------------------------
// 辅助函数 (cfa 前缀)
// ---------------------------------------------------------------------------

// cfaTokenize 将字符串切分为 token 列表。
// ASCII 字母数字按单词聚合（小写化），非 ASCII 字符（如中文）作为独立 token。
func cfaTokenize(s string) []string {
	tokens := make([]string, 0)
	var buf []rune
	for _, r := range s {
		if r >= 'A' && r <= 'Z' {
			r = r + ('a' - 'A')
		}
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			buf = append(buf, r)
			continue
		}
		if len(buf) > 0 {
			tokens = append(tokens, string(buf))
			buf = buf[:0]
		}
		if r >= 0x80 {
			tokens = append(tokens, string(r))
		}
	}
	if len(buf) > 0 {
		tokens = append(tokens, string(buf))
	}
	return tokens
}

// cfaSet 将 token 列表转为集合。
func cfaSet(tokens []string) map[string]bool {
	m := make(map[string]bool, len(tokens))
	for _, t := range tokens {
		m[t] = true
	}
	return m
}

// cfaJaccard 计算两组 token 的 Jaccard 相似度。
// 两集合均为空时返回 1.0（视为完全相同）。
func cfaJaccard(a, b []string) float64 {
	setA := cfaSet(a)
	setB := cfaSet(b)

	intersect := 0
	for k := range setA {
		if setB[k] {
			intersect++
		}
	}
	union := len(setA) + len(setB) - intersect
	if union == 0 {
		return 1.0
	}
	return float64(intersect) / float64(union)
}

// cfaIsBreak 当相似度 < 0.2 时判定为流断裂。
func cfaIsBreak(sim float64) bool {
	return sim < 0.2
}

// cfaClamp 将值限制在 [lo, hi] 范围内。
func cfaClamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// cfaUpdateAvg 以增量方式更新平均值。
// count 为包含当前样本在内的样本总数。
func cfaUpdateAvg(avg float64, count int, value float64) float64 {
	if count <= 0 {
		return value
	}
	return avg + (value-avg)/float64(count)
}

// cfaCategory 将流分数映射为类别字符串。
func cfaCategory(score float64) string {
	switch {
	case score < 0.3:
		return "disjointed"
	case score < 0.6:
		return "interrupted"
	case score < 0.8:
		return "smooth"
	default:
		return "seamless"
	}
}

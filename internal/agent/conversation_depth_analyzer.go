package agent

import "sync"

// ── OPT-116: ConversationDepthAnalyzer (对话深度分析器) ──
// 分析对话的复杂度和深度，基于消息数量、平均消息长度和话题转换次数
// 计算深度值（0-100），用于指导上下文压缩和缓存策略。
//
// 原理：对话深度与消息数量、单条消息的信息密度以及话题跳转频率正相关。
// 通过对相邻消息进行关键词集合的 Jaccard 相似度比较，当相似度低于阈值
// 时判定为一次话题转换。三个因子加权求和后映射到 0-100 的深度值。
//
// 效果：为上下文压缩器、缓存策略等模块提供对话复杂度的量化指标，
// 使优化策略能根据深度自适应调整。

// ConversationDepthAnalyzer 对话深度分析器
type ConversationDepthAnalyzer struct {
	mu               sync.RWMutex
	totalAnalyses    int
	maxDepthObserved int
	avgDepth         float64
	depthHistory     []int
	maxHistorySize   int
}

// NewConversationDepthAnalyzer 创建对话深度分析器，maxHistorySize 默认为 50。
func NewConversationDepthAnalyzer() *ConversationDepthAnalyzer {
	return &ConversationDepthAnalyzer{
		maxHistorySize: 50,
		depthHistory:   make([]int, 0, 50),
	}
}

// AnalyzeDepth 分析对话深度。
// 基于消息数量、平均消息长度和话题转换次数（通过关键词变化检测）
// 计算深度值（0-100），并更新历史记录与统计信息。
//
// 深度计算因子：
//   - 消息数量得分（0-40）：每条消息贡献 5 分，上限 40
//   - 平均消息长度得分（0-30）：每 10 字符贡献 3 分，上限 30
//   - 话题转换次数得分（0-30）：每次转换贡献 10 分，上限 30
func (a *ConversationDepthAnalyzer) AnalyzeDepth(messages []string) int {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.totalAnalyses++

	depth := cdaCalculateDepth(messages)

	if depth > a.maxDepthObserved {
		a.maxDepthObserved = depth
	}

	// 更新滚动平均
	a.avgDepth = (a.avgDepth*float64(a.totalAnalyses-1) + float64(depth)) / float64(a.totalAnalyses)

	// 追加到历史记录，超出上限时裁剪
	a.depthHistory = append(a.depthHistory, depth)
	if len(a.depthHistory) > a.maxHistorySize {
		a.depthHistory = a.depthHistory[len(a.depthHistory)-a.maxHistorySize:]
	}

	return depth
}

// GetDepthCategory 根据深度值返回分类标签。
// 0-20: shallow, 21-40: moderate, 41-60: deep, 61-80: complex, 81-100: expert
func (a *ConversationDepthAnalyzer) GetDepthCategory(depth int) string {
	return cdaDepthCategory(depth)
}

// GetStats 返回分析器的统计信息，包括 totalAnalyses、maxDepthObserved、
// avgDepth、lastCategory 和 historySize。
func (a *ConversationDepthAnalyzer) GetStats() map[string]interface{} {
	a.mu.RLock()
	defer a.mu.RUnlock()

	lastCategory := ""
	if len(a.depthHistory) > 0 {
		lastCategory = cdaDepthCategory(a.depthHistory[len(a.depthHistory)-1])
	}

	return map[string]interface{}{
		"totalAnalyses":    a.totalAnalyses,
		"maxDepthObserved": a.maxDepthObserved,
		"avgDepth":         a.avgDepth,
		"lastCategory":     lastCategory,
		"historySize":      len(a.depthHistory),
	}
}

// Reset 重置分析器，清除所有统计与历史记录。
func (a *ConversationDepthAnalyzer) Reset() {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.totalAnalyses = 0
	a.maxDepthObserved = 0
	a.avgDepth = 0
	a.depthHistory = nil
}

// ---------------------------------------------------------------------------
// 内部辅助函数（以 cda 为前缀，避免命名冲突）
// ---------------------------------------------------------------------------

// cdaCalculateDepth 计算对话深度值（0-100）。
// 深度由三个因子加权得出：
//   - 消息数量得分（0-40）
//   - 平均消息长度得分（0-30）
//   - 话题转换次数得分（0-30）
func cdaCalculateDepth(messages []string) int {
	if len(messages) == 0 {
		return 0
	}

	// 消息数量得分：每条消息贡献 5 分，上限 40
	msgCountScore := len(messages) * 5
	if msgCountScore > 40 {
		msgCountScore = 40
	}

	// 平均消息长度得分
	totalLen := 0
	for _, msg := range messages {
		totalLen += len(msg)
	}
	avgLen := totalLen / len(messages)
	// 每 10 字符贡献 3 分，上限 30
	avgLenScore := (avgLen / 10) * 3
	if avgLenScore > 30 {
		avgLenScore = 30
	}

	// 话题转换次数得分
	transitions := cdaCountTopicTransitions(messages)
	transitionScore := transitions * 10
	if transitionScore > 30 {
		transitionScore = 30
	}

	depth := msgCountScore + avgLenScore + transitionScore
	if depth > 100 {
		depth = 100
	}
	return depth
}

// cdaCountTopicTransitions 统计消息间的话题转换次数。
// 通过比较相邻消息的关键词集合，若 Jaccard 相似度低于 0.3 则视为话题转换。
func cdaCountTopicTransitions(messages []string) int {
	if len(messages) < 2 {
		return 0
	}

	tokenSets := make([]map[string]struct{}, len(messages))
	for i, msg := range messages {
		tokenSets[i] = cdaTokenize(msg)
	}

	transitions := 0
	for i := 1; i < len(tokenSets); i++ {
		sim := cdaJaccardSimilarity(tokenSets[i-1], tokenSets[i])
		if sim < 0.3 {
			transitions++
		}
	}
	return transitions
}

// cdaTokenize 将字符串按空白分词为小写 token 集合。
func cdaTokenize(s string) map[string]struct{} {
	set := make(map[string]struct{})
	current := make([]rune, 0, len(s))
	for _, ch := range s {
		if ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r' {
			if len(current) > 0 {
				set[string(current)] = struct{}{}
				current = current[:0]
			}
		} else {
			current = append(current, ch)
		}
	}
	if len(current) > 0 {
		set[string(current)] = struct{}{}
	}
	return set
}

// cdaJaccardSimilarity 计算两个 token 集合的 Jaccard 相似度。
func cdaJaccardSimilarity(a, b map[string]struct{}) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 0
	}
	intersection := 0
	for t := range a {
		if _, ok := b[t]; ok {
			intersection++
		}
	}
	union := len(a) + len(b) - intersection
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}

// cdaDepthCategory 将深度值映射为分类标签。
// 0-20: shallow, 21-40: moderate, 41-60: deep, 61-80: complex, 81-100: expert
func cdaDepthCategory(depth int) string {
	switch {
	case depth <= 20:
		return "shallow"
	case depth <= 40:
		return "moderate"
	case depth <= 60:
		return "deep"
	case depth <= 80:
		return "complex"
	default:
		return "expert"
	}
}

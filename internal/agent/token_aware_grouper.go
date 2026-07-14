package agent

import "sync"

// ── OPT-141: TokenAwareGrouper (Token 感知分组器) ──
// 将消息按 token 效率分组，每组总 token 不超过阈值。
// 遍历消息列表，依次累加 token；当加入当前项会超出阈值时，
// 先保存当前组再开启新组。保证每个消息必定归属某个分组。

// GrouperItem 分组器中的一个消息项。
type GrouperItem struct {
	Content string
	Tokens  int
}

// TokenAwareGrouper Token 感知分组器，将消息按 token 效率分组。
type TokenAwareGrouper struct {
	mu                 sync.RWMutex
	totalGroups        int
	totalMessages      int
	totalTokensGrouped int
	groupThreshold     int
}

// NewTokenAwareGrouper 创建一个新的 Token 感知分组器。
// threshold 为每组允许的最大 token 数。
func NewTokenAwareGrouper(threshold int) *TokenAwareGrouper {
	return &TokenAwareGrouper{
		groupThreshold: threshold,
	}
}

// Group 将消息分组，每组总 token 不超过 threshold。
// 返回二维切片，每个子切片代表一个分组。
func (g *TokenAwareGrouper) Group(messages []GrouperItem) [][]GrouperItem {
	g.mu.Lock()
	defer g.mu.Unlock()

	var groups [][]GrouperItem
	var currentGroup []GrouperItem
	currentTokens := 0

	for _, msg := range messages {
		if currentTokens+msg.Tokens > g.groupThreshold && len(currentGroup) > 0 {
			groups = append(groups, currentGroup)
			currentGroup = nil
			currentTokens = 0
		}
		currentGroup = append(currentGroup, msg)
		currentTokens += msg.Tokens
	}

	if len(currentGroup) > 0 {
		groups = append(groups, currentGroup)
	}

	g.totalGroups = len(groups)
	g.totalMessages = len(messages)
	g.totalTokensGrouped = tgrSumTokens(messages)

	return groups
}

// GetGroupCount 返回当前分组数量。
func (g *TokenAwareGrouper) GetGroupCount() int {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.totalGroups
}

// GetStats 返回分组器的统计信息。
// 包含: totalGroups, totalMessages, totalTokensGrouped, avgGroupSize, avgGroupTokens。
func (g *TokenAwareGrouper) GetStats() map[string]interface{} {
	g.mu.RLock()
	defer g.mu.RUnlock()

	avgSize, avgTokens := tgrComputeAverages(g.totalGroups, g.totalMessages, g.totalTokensGrouped)
	return map[string]interface{}{
		"totalGroups":        g.totalGroups,
		"totalMessages":      g.totalMessages,
		"totalTokensGrouped": g.totalTokensGrouped,
		"avgGroupSize":       avgSize,
		"avgGroupTokens":     avgTokens,
	}
}

// Reset 重置分组器的所有统计信息。
func (g *TokenAwareGrouper) Reset() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.totalGroups = 0
	g.totalMessages = 0
	g.totalTokensGrouped = 0
}

// tgrSumTokens 计算消息列表的总 token 数。
func tgrSumTokens(items []GrouperItem) int {
	total := 0
	for _, item := range items {
		total += item.Tokens
	}
	return total
}

// tgrComputeAverages 根据分组数计算平均组大小和平均组 token 数。
func tgrComputeAverages(totalGroups, totalMessages, totalTokens int) (float64, float64) {
	if totalGroups == 0 {
		return 0, 0
	}
	return float64(totalMessages) / float64(totalGroups), float64(totalTokens) / float64(totalGroups)
}

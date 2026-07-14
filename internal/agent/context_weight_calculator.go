package agent

import "sync"

// ── OPT-121: ContextWeightCalculator (上下文权重计算器) ──
// 计算每条消息在上下文中的重要性权重，综合考虑位置、长度和新鲜度
// 三个因子，为消息保留/压缩决策提供量化依据。
//
// 权重公式：
//   weight = 0.3 * positionFactor + 0.3 * lengthFactor + 0.4 * freshnessFactor
//
// - positionFactor: 首条消息权重最高（1.0），随位置递减
// - lengthFactor:   消息长度 / 200，上限 1.0
// - freshnessFactor: 越靠后的消息越新鲜，position/totalMessages

// ContextWeightCalculator 上下文权重计算器，计算每条消息的重要性权重。
type ContextWeightCalculator struct {
	mu                sync.RWMutex
	totalCalculations int
	totalWeight       float64
	weightHistory     []float64
	maxHistorySize    int
}

// NewContextWeightCalculator 创建一个新的上下文权重计算器实例。
// 默认历史记录最大容量为 50。
func NewContextWeightCalculator() *ContextWeightCalculator {
	return &ContextWeightCalculator{
		maxHistorySize: 50,
		weightHistory:  make([]float64, 0, 50),
	}
}

// CalculateWeight 计算单条消息的重要性权重。
//
// 权重 = 位置因子(0.3, 首条消息权重高)
//   - 长度因子(0.3, len/200 上限 1.0)
//   - 新鲜度因子(0.4, position/totalMessages)
//
// 每次调用会更新内部统计（总计算次数、累计权重、历史记录）。
func (c *ContextWeightCalculator) CalculateWeight(message string, position int, totalMessages int) float64 {
	c.mu.Lock()
	defer c.mu.Unlock()

	positionFactor := cwcPositionFactor(position, totalMessages)
	lengthFactor := cwcLengthFactor(message)
	freshnessFactor := cwcFreshnessFactor(position, totalMessages)

	weight := 0.3*positionFactor + 0.3*lengthFactor + 0.4*freshnessFactor

	c.totalCalculations++
	c.totalWeight += weight
	c.weightHistory = append(c.weightHistory, weight)
	if len(c.weightHistory) > c.maxHistorySize {
		c.weightHistory = c.weightHistory[len(c.weightHistory)-c.maxHistorySize:]
	}

	return weight
}

// GetWeightCategory 根据权重值返回权重类别字符串。
// <0.3 → "low", <0.6 → "medium", <0.8 → "high", 否则 → "critical"。
func (c *ContextWeightCalculator) GetWeightCategory(weight float64) string {
	if weight < 0.3 {
		return "low"
	}
	if weight < 0.6 {
		return "medium"
	}
	if weight < 0.8 {
		return "high"
	}
	return "critical"
}

// GetStats 返回计算器的统计信息，包括总计算次数、平均权重、最近类别和历史记录大小。
func (c *ContextWeightCalculator) GetStats() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	stats := make(map[string]interface{})
	stats["totalCalculations"] = c.totalCalculations

	avgWeight := 0.0
	if c.totalCalculations > 0 {
		avgWeight = c.totalWeight / float64(c.totalCalculations)
	}
	stats["avgWeight"] = avgWeight

	lastCategory := "unknown"
	if len(c.weightHistory) > 0 {
		lastCategory = c.GetWeightCategory(c.weightHistory[len(c.weightHistory)-1])
	}
	stats["lastCategory"] = lastCategory
	stats["historySize"] = len(c.weightHistory)

	return stats
}

// Reset 重置计算器的所有统计数据和历史记录。
func (c *ContextWeightCalculator) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.totalCalculations = 0
	c.totalWeight = 0
	c.weightHistory = make([]float64, 0, c.maxHistorySize)
}

// cwcPositionFactor 计算位置因子，首条消息权重最高（返回值趋近 1.0）。
func cwcPositionFactor(position int, totalMessages int) float64 {
	if totalMessages <= 0 {
		return 0
	}
	return 1.0 - float64(position)/float64(totalMessages)
}

// cwcLengthFactor 计算长度因子，消息长度除以 200，上限为 1.0。
func cwcLengthFactor(message string) float64 {
	factor := float64(len(message)) / 200.0
	if factor > 1.0 {
		return 1.0
	}
	return factor
}

// cwcFreshnessFactor 计算新鲜度因子，越靠后的消息越新鲜。
func cwcFreshnessFactor(position int, totalMessages int) float64 {
	if totalMessages <= 0 {
		return 0
	}
	return float64(position) / float64(totalMessages)
}

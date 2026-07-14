package agent

import (
	"sort"
	"sync"
)

// ── OPT-135: ContextAssemblyOptimizer (上下文组装优化器) ──
// 优化上下文消息的组装顺序，将消息按优先级分组排序:
//   1. system 消息在前
//   2. 高价值消息在中
//   3. 最近消息在后
//   4. 废弃消息在最后
//
// 原理：通过对消息分组并按组内关键字排序，使最重要的上下文
// 出现在 prompt 前部，提升模型对关键信息的关注度。

// caoHighValueThreshold 定义高价值消息的 Value 阈值。
const caoHighValueThreshold = 5

// AssemblyItem 表示一个待组装的上下文消息条目。
type AssemblyItem struct {
	Content string
	Role    string
	Value   int
	Turn    int
}

// ContextAssemblyOptimizer 上下文组装优化器，优化上下文消息的组装顺序。
type ContextAssemblyOptimizer struct {
	mu                 sync.RWMutex
	totalAssemblies    int
	totalReorderings   int
	avgAssemblyTime    float64
	assemblyStrategies map[string]int
}

// NewContextAssemblyOptimizer 创建一个新的上下文组装优化器实例。
func NewContextAssemblyOptimizer() *ContextAssemblyOptimizer {
	return &ContextAssemblyOptimizer{
		assemblyStrategies: make(map[string]int),
	}
}

// Assemble 按优先级组装消息列表。
// 排序规则: system 消息在前，高价值消息在中，最近消息在后，废弃消息在最后。
// 返回排序后的消息列表副本。
func (o *ContextAssemblyOptimizer) Assemble(messages []AssemblyItem) []AssemblyItem {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.totalAssemblies++

	n := len(messages)
	if n == 0 {
		return []AssemblyItem{}
	}

	// Copy messages
	result := make([]AssemblyItem, n)
	copy(result, messages)

	// Sort by assembly priority
	sort.Slice(result, func(i, j int) bool {
		pi := caoPriority(result[i])
		pj := caoPriority(result[j])
		if pi != pj {
			return pi < pj
		}
		// Within same priority group, sort by specific key
		switch pi {
		case 1: // high-value: sort by Value descending
			return result[i].Value > result[j].Value
		case 2: // recent: sort by Turn descending (most recent first)
			return result[i].Turn > result[j].Turn
		default:
			return false
		}
	})

	// Count reorderings (items that moved position)
	reorderings := 0
	for i := 0; i < n; i++ {
		if result[i] != messages[i] {
			reorderings++
		}
	}
	o.totalReorderings += reorderings

	// Track assembly time (synthetic cost based on message count)
	o.avgAssemblyTime = caoUpdateAvgTime(o.avgAssemblyTime, o.totalAssemblies, float64(n))

	// Track strategy usage
	strategy := caoGetStrategy(result)
	o.assemblyStrategies[strategy]++

	return result
}

// GetAssemblyStrategy 分析消息列表并返回适用的组装策略。
// 返回值: "system_first" / "value_weighted" / "recency_sorted" / "default"。
func (o *ContextAssemblyOptimizer) GetAssemblyStrategy(messages []AssemblyItem) string {
	return caoGetStrategy(messages)
}

// GetStats 返回组装优化器的统计信息。
func (o *ContextAssemblyOptimizer) GetStats() map[string]interface{} {
	o.mu.RLock()
	defer o.mu.RUnlock()

	return map[string]interface{}{
		"totalAssemblies":  o.totalAssemblies,
		"totalReorderings": o.totalReorderings,
		"avgAssemblyTime":  o.avgAssemblyTime,
		"strategyCount":    len(o.assemblyStrategies),
	}
}

// Reset 重置组装优化器的所有统计数据。
func (o *ContextAssemblyOptimizer) Reset() {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.totalAssemblies = 0
	o.totalReorderings = 0
	o.avgAssemblyTime = 0
	o.assemblyStrategies = make(map[string]int)
}

// ---------------------------------------------------------------------------
// 辅助函数 (cao 前缀)
// ---------------------------------------------------------------------------

// caoPriority 返回消息的组装优先级组。
// 0: system, 1: high-value, 2: recent, 3: deprecated。
func caoPriority(item AssemblyItem) int {
	switch item.Role {
	case "system":
		return 0
	case "deprecated", "stale":
		return 3
	}
	if item.Value >= caoHighValueThreshold {
		return 1
	}
	return 2
}

// caoGetStrategy 分析消息列表并返回适用的组装策略。
// 若包含 system 消息则返回 "system_first"，
// 若存在 Value 差异则返回 "value_weighted"，
// 若已按 Turn 降序排列则返回 "recency_sorted"，
// 否则返回 "default"。
func caoGetStrategy(messages []AssemblyItem) string {
	if len(messages) == 0 {
		return "default"
	}

	systemCount := 0
	minValue := messages[0].Value
	maxValue := messages[0].Value

	for _, m := range messages {
		if m.Role == "system" {
			systemCount++
		}
		if m.Value < minValue {
			minValue = m.Value
		}
		if m.Value > maxValue {
			maxValue = m.Value
		}
	}

	if systemCount > 0 {
		return "system_first"
	}

	if maxValue-minValue > 0 {
		return "value_weighted"
	}

	// Check if already sorted by Turn descending (recency)
	isRecencySorted := true
	for i := 1; i < len(messages); i++ {
		if messages[i-1].Turn < messages[i].Turn {
			isRecencySorted = false
			break
		}
	}
	if isRecencySorted {
		return "recency_sorted"
	}

	return "default"
}

// caoUpdateAvgTime 更新运行平均组装时间。
// 采用增量公式: (oldAvg * (n-1) + newCost) / n。
func caoUpdateAvgTime(currentAvg float64, totalAssemblies int, assemblyCost float64) float64 {
	if totalAssemblies <= 0 {
		return 0
	}
	return (currentAvg*float64(totalAssemblies-1) + assemblyCost) / float64(totalAssemblies)
}

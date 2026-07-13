package agent

import "sync"

// ── OPT-42: 流式 Token 守卫 (Streaming Token Guard) ──
// 在流式输出过程中实时监控 token 消耗，防止超出预算。
//
// 原理：流式生成时 token 是逐个产出的，如果不做实时监控，
// 可能在一个 turn 内就耗尽全部预算。StreamingTokenGuard 在
// 每次 token 写入时累加计数，并与预算上限比较：
// - 当使用率达到 warningThreshold（默认 75%）时发出警告
// - 当使用率达到 criticalThreshold（默认 90%）时建议终止
//
// 效果：避免单 turn token 爆炸，在接近预算上限时提前预警，
// 让上层逻辑有机会优雅终止流式输出而非超时失败。

// GuardStatus 预算检查状态
type GuardStatus struct {
	Status          string  // "ok" / "warning" / "critical"
	UsageRatio      float64 // 当前使用率 (0.0 ~ 1.0+)
	RemainingTokens int     // 剩余可用 token
	Message         string  // 人类可读的状态描述
}

// StreamingGuardStats 守卫聚合统计
type StreamingGuardStats struct {
	TurnsMonitored  int     // 已监控的 turn 数
	TotalWarnings   int     // 累计警告次数
	TotalCritical   int     // 累计临界次数
	AvgInputTokens  float64 // 平均每 turn 输入 token
	AvgOutputTokens float64 // 平均每 turn 输出 token
}

// StreamingTokenGuard 流式 token 守卫
// 监控实时 token 消耗，在接近预算上限时发出预警。
type StreamingTokenGuard struct {
	mu sync.RWMutex

	// 当前 turn 的 token 计数
	turnInputTokens  int
	turnOutputTokens int

	// 预算配置
	budgetLimit       int
	warningThreshold  float64 // 警告阈值（默认 0.75）
	criticalThreshold float64 // 临界阈值（默认 0.90）

	// 累计统计
	warningsEmitted int
	criticalEmitted int
	turnsMonitored  int

	// 内部追踪：跨 turn 累计用于计算平均值
	totalInputTokens  int
	totalOutputTokens int

	// 内部追踪：当前 turn 是否已发出过警告/临界，避免重复计数
	warningEmittedThisTurn  bool
	criticalEmittedThisTurn bool
}

// NewStreamingTokenGuard 创建流式 token 守卫。
// 默认 warningThreshold=0.75, criticalThreshold=0.90。
func NewStreamingTokenGuard(budgetLimit int) *StreamingTokenGuard {
	return &StreamingTokenGuard{
		budgetLimit:       budgetLimit,
		warningThreshold:  0.75,
		criticalThreshold: 0.90,
	}
}

// RecordInput 记录输入 token，累加到当前 turn 的输入计数。
func (g *StreamingTokenGuard) RecordInput(tokens int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.turnInputTokens += tokens
}

// RecordOutput 记录输出 token，累加到当前 turn 的输出计数。
func (g *StreamingTokenGuard) RecordOutput(tokens int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.turnOutputTokens += tokens
}

// CheckBudget 检查当前 token 使用率是否接近或超出预算。
// 返回 GuardStatus，同时在首次进入 warning/critical 状态时
// 递增对应的累计计数器。
func (g *StreamingTokenGuard) CheckBudget() GuardStatus {
	g.mu.Lock()
	defer g.mu.Unlock()

	totalTokens := g.turnInputTokens + g.turnOutputTokens

	var usageRatio float64
	if g.budgetLimit > 0 {
		usageRatio = float64(totalTokens) / float64(g.budgetLimit)
	}

	remaining := g.budgetLimit - totalTokens
	if remaining < 0 {
		remaining = 0
	}

	var status, message string
	switch {
	case usageRatio >= g.criticalThreshold:
		status = "critical"
		message = "token 使用率已超过临界阈值，建议终止当前流式输出"
		if !g.criticalEmittedThisTurn {
			g.criticalEmitted++
			g.criticalEmittedThisTurn = true
		}
	case usageRatio >= g.warningThreshold:
		status = "warning"
		message = "token 使用率接近预算上限，请注意控制输出长度"
		if !g.warningEmittedThisTurn {
			g.warningsEmitted++
			g.warningEmittedThisTurn = true
		}
	default:
		status = "ok"
		message = "token 使用率处于正常范围"
	}

	return GuardStatus{
		Status:          status,
		UsageRatio:      usageRatio,
		RemainingTokens: remaining,
		Message:         message,
	}
}

// ShouldTerminate 返回是否应终止当前流式输出。
// 当使用率超过 criticalThreshold 时返回 true。
func (g *StreamingTokenGuard) ShouldTerminate() bool {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if g.budgetLimit <= 0 {
		return false
	}

	totalTokens := g.turnInputTokens + g.turnOutputTokens
	usageRatio := float64(totalTokens) / float64(g.budgetLimit)
	return usageRatio >= g.criticalThreshold
}

// ResetTurn 重置当前 turn 的计数器，并将本 turn 的 token 数
// 累加到跨 turn 总计中用于后续平均值计算，同时递增 turnsMonitored。
func (g *StreamingTokenGuard) ResetTurn() {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.totalInputTokens += g.turnInputTokens
	g.totalOutputTokens += g.turnOutputTokens

	g.turnInputTokens = 0
	g.turnOutputTokens = 0
	g.warningEmittedThisTurn = false
	g.criticalEmittedThisTurn = false
	g.turnsMonitored++
}

// GetStats 返回跨所有已监控 turn 的聚合统计信息。
func (g *StreamingTokenGuard) GetStats() StreamingGuardStats {
	g.mu.RLock()
	defer g.mu.RUnlock()

	var avgInput, avgOutput float64
	if g.turnsMonitored > 0 {
		avgInput = float64(g.totalInputTokens) / float64(g.turnsMonitored)
		avgOutput = float64(g.totalOutputTokens) / float64(g.turnsMonitored)
	}

	return StreamingGuardStats{
		TurnsMonitored:  g.turnsMonitored,
		TotalWarnings:   g.warningsEmitted,
		TotalCritical:   g.criticalEmitted,
		AvgInputTokens:  avgInput,
		AvgOutputTokens: avgOutput,
	}
}

package agent

import "sync"

// ── OPT-117: TokenEfficiencyMonitor (Token 效率监控器) ──
// 实时监控 token 使用效率，记录输入、输出、缓存和浪费的 token 数量，
// 计算效率指标并追踪效率趋势变化。
//
// 原理：效率 = (output + cached) / (input + output + wasted + cached)。
// 通过持续记录监控点，可以追踪效率随时间的变化趋势（上升/下降/稳定），
// 为自动调参和告警提供数据支撑。
//
// 效果：量化 token 使用效率，识别浪费热点，指导缓存与压缩策略的优化。

// TokenEfficiencyMonitor Token 效率监控器
type TokenEfficiencyMonitor struct {
	mu                sync.RWMutex
	totalInput        int
	totalOutput       int
	totalCached       int
	totalWasted       int
	monitoringPoints  int
	efficiencyHistory []float64
	maxHistorySize    int
}

// NewTokenEfficiencyMonitor 创建 Token 效率监控器，maxHistorySize 默认为 100。
func NewTokenEfficiencyMonitor() *TokenEfficiencyMonitor {
	return &TokenEfficiencyMonitor{
		maxHistorySize:    100,
		efficiencyHistory: make([]float64, 0, 100),
	}
}

// RecordPoint 记录一个监控点。
// 累加输入、输出、缓存和浪费的 token 数量，计算当前效率并追加到历史记录。
func (m *TokenEfficiencyMonitor) RecordPoint(input, output, cached, wasted int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.totalInput += input
	m.totalOutput += output
	m.totalCached += cached
	m.totalWasted += wasted
	m.monitoringPoints++

	efficiency := temCalcEfficiency(input, output, cached, wasted)
	m.efficiencyHistory = append(m.efficiencyHistory, efficiency)
	if len(m.efficiencyHistory) > m.maxHistorySize {
		m.efficiencyHistory = m.efficiencyHistory[len(m.efficiencyHistory)-m.maxHistorySize:]
	}
}

// CalculateEfficiency 计算总体 token 效率。
// 公式：(output + cached) / (input + output + wasted + cached)
// 若分母为 0 则返回 0。
func (m *TokenEfficiencyMonitor) CalculateEfficiency() float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return temCalcEfficiency(m.totalInput, m.totalOutput, m.totalCached, m.totalWasted)
}

// GetEfficiencyTrend 根据最近 5 个监控点的效率趋势返回标签。
// 返回 "improving"（上升）、"declining"（下降）或 "stable"（稳定）。
func (m *TokenEfficiencyMonitor) GetEfficiencyTrend() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return temCalcTrend(m.efficiencyHistory)
}

// GetStats 返回监控器的统计信息，包括 totalInput、totalOutput、
// totalCached、totalWasted、efficiency、monitoringPoints 和 trend。
func (m *TokenEfficiencyMonitor) GetStats() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	efficiency := temCalcEfficiency(m.totalInput, m.totalOutput, m.totalCached, m.totalWasted)
	trend := temCalcTrend(m.efficiencyHistory)

	return map[string]interface{}{
		"totalInput":       m.totalInput,
		"totalOutput":      m.totalOutput,
		"totalCached":      m.totalCached,
		"totalWasted":      m.totalWasted,
		"efficiency":       efficiency,
		"monitoringPoints": m.monitoringPoints,
		"trend":            trend,
	}
}

// Reset 重置监控器，清除所有累计数据与历史记录。
func (m *TokenEfficiencyMonitor) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.totalInput = 0
	m.totalOutput = 0
	m.totalCached = 0
	m.totalWasted = 0
	m.monitoringPoints = 0
	m.efficiencyHistory = nil
}

// ---------------------------------------------------------------------------
// 内部辅助函数（以 tem 为前缀，避免命名冲突）
// ---------------------------------------------------------------------------

// temCalcEfficiency 计算效率：(output + cached) / (input + output + wasted + cached)。
// 若分母为 0 则返回 0。
func temCalcEfficiency(input, output, cached, wasted int) float64 {
	denominator := input + output + wasted + cached
	if denominator == 0 {
		return 0
	}
	return float64(output+cached) / float64(denominator)
}

// temCalcTrend 根据历史效率值判断趋势。
// 取最近 5 个点，比较后半段与前半段的平均值：
//   - 后半段均值 > 前半段均值 * 1.05 → "improving"
//   - 后半段均值 < 前半段均值 * 0.95 → "declining"
//   - 否则 → "stable"
func temCalcTrend(history []float64) string {
	if len(history) < 2 {
		return "stable"
	}

	// 取最近 5 个点
	recent := history
	if len(recent) > 5 {
		recent = recent[len(recent)-5:]
	}

	mid := len(recent) / 2
	if mid == 0 {
		return "stable"
	}

	var firstHalfSum, secondHalfSum float64
	for i := 0; i < mid; i++ {
		firstHalfSum += recent[i]
	}
	for i := mid; i < len(recent); i++ {
		secondHalfSum += recent[i]
	}

	firstHalfAvg := firstHalfSum / float64(mid)
	secondHalfAvg := secondHalfSum / float64(len(recent)-mid)

	if secondHalfAvg > firstHalfAvg*1.05 {
		return "improving"
	}
	if secondHalfAvg < firstHalfAvg*0.95 {
		return "declining"
	}
	return "stable"
}

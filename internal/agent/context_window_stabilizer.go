package agent

import (
	"math"
	"sync"
)

// ── OPT-253: ContextWindowStabilizer (上下文窗口稳定器 / Context Window Stabilizer) ──
// 通过记录历史观察值并基于历史平均值平滑上下文窗口大小，减少抖动。
// 稳定度分数反映历史观察值相对于平均值的离散程度（越接近 1 越稳定）。
//
// 原理：记录最近若干次观察到的窗口大小，Stabilize 基于历史平均值
// 计算稳定后的窗口大小，避免单次波动导致窗口剧烈变化。
// 稳定度分数 = 1 - (标准差 / 平均值)，结果限制在 [0, 1] 范围。
//
// 效果：平滑上下文窗口大小，统计调整次数和稳定度，
// 为上下文管理提供数据支撑。

// ContextWindowStabilizer 上下文窗口稳定器
type ContextWindowStabilizer struct {
	mu             sync.RWMutex
	targetSize     int     // 目标窗口大小
	currentSize    int     // 当前稳定后的窗口大小
	stabilityScore float64 // 稳定度分数 [0,1]
	adjustments    int     // 调整次数
	history        []int   // 历史观察值
	maxHistory     int     // 历史最大容量
}

// NewContextWindowStabilizer 创建上下文窗口稳定器。
// targetSize 指定目标窗口大小，maxHistory 指定历史容量。
// 若 targetSize <= 0 则默认 8192，若 maxHistory <= 0 则默认 16。
// 初始 currentSize 等于 targetSize。
func NewContextWindowStabilizer(targetSize int, maxHistory int) *ContextWindowStabilizer {
	if targetSize <= 0 {
		targetSize = 8192
	}
	if maxHistory <= 0 {
		maxHistory = 16
	}
	return &ContextWindowStabilizer{
		targetSize:  targetSize,
		currentSize: targetSize,
		history:     make([]int, 0, maxHistory),
		maxHistory:  maxHistory,
	}
}

// Record 记录观察到的窗口大小，追加到历史（超出容量时丢弃最旧）。
func (c *ContextWindowStabilizer) Record(observedSize int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.history = append(c.history, observedSize)
	if len(c.history) > c.maxHistory {
		c.history = c.history[len(c.history)-c.maxHistory:]
	}
}

// Stabilize 计算稳定后的窗口大小（基于历史平均值）。
// 更新 currentSize 和 stabilityScore，并递增 adjustments。
// 若无历史数据则返回 targetSize 且 stabilityScore 设为 1.0。
func (c *ContextWindowStabilizer) Stabilize() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.adjustments++
	if len(c.history) == 0 {
		c.stabilityScore = 1.0
		return c.targetSize
	}

	avg := cwsComputeAvg(c.history)
	c.currentSize = avg
	c.stabilityScore = cwsComputeStability(c.history, avg)
	return c.currentSize
}

// GetStabilityScore 获取稳定度分数。
func (c *ContextWindowStabilizer) GetStabilityScore() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.stabilityScore
}

// GetStats 返回稳定器的统计信息。
// 包含 targetSize、currentSize、stabilityScore、adjustments 和 historySize。
func (c *ContextWindowStabilizer) GetStats() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return map[string]interface{}{
		"targetSize":     c.targetSize,
		"currentSize":    c.currentSize,
		"stabilityScore": c.stabilityScore,
		"adjustments":    c.adjustments,
		"historySize":    len(c.history),
	}
}

// Reset 重置稳定器的状态和计数（保留 targetSize 和 maxHistory 配置）。
func (c *ContextWindowStabilizer) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.currentSize = c.targetSize
	c.stabilityScore = 0
	c.adjustments = 0
	c.history = make([]int, 0, c.maxHistory)
}

// cwsComputeAvg 计算历史观察值的平均值（整数除法）。
func cwsComputeAvg(history []int) int {
	if len(history) == 0 {
		return 0
	}
	sum := 0
	for _, v := range history {
		sum += v
	}
	return sum / len(history)
}

// cwsComputeStability 计算稳定度分数 = 1 - (标准差 / 平均值)。
// 结果限制在 [0, 1] 范围内。若平均值为 0 或无历史则返回 1.0。
func cwsComputeStability(history []int, avg int) float64 {
	if len(history) == 0 || avg == 0 {
		return 1.0
	}
	var sumSqDiff float64
	for _, v := range history {
		diff := float64(v - avg)
		sumSqDiff += diff * diff
	}
	variance := sumSqDiff / float64(len(history))
	stddev := math.Sqrt(variance)
	score := 1.0 - stddev/float64(avg)
	if score < 0 {
		score = 0
	}
	if score > 1 {
		score = 1
	}
	return score
}

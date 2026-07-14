package agent

import "sync"

// OPT-198: ContextWindowCalibratorV2 / 上下文窗口校准器V2
// 根据实际Token使用量动态校准上下文窗口大小，保持在[min, max]范围内。

// ContextWindowCalibratorV2 是上下文窗口校准器V2。
type ContextWindowCalibratorV2 struct {
	mu               sync.RWMutex
	targetTokens     int
	minTokens        int
	maxTokens        int
	calibrationCount int
	adjustments      []int
}

// NewContextWindowCalibratorV2 创建一个新的ContextWindowCalibratorV2实例。
func NewContextWindowCalibratorV2(target int, minTokens int, maxTokens int) *ContextWindowCalibratorV2 {
	clampedTarget := cwc2Clamp(target, minTokens, maxTokens)
	return &ContextWindowCalibratorV2{
		targetTokens:     clampedTarget,
		minTokens:        minTokens,
		maxTokens:        maxTokens,
		calibrationCount: 0,
		adjustments:      make([]int, 0),
	}
}

// Calibrate 根据实际使用量校准窗口大小，返回校准后的目标Token数。
func (c *ContextWindowCalibratorV2) Calibrate(actualUsage int) int {
	c.mu.Lock()
	defer c.mu.Unlock()

	var delta int
	if actualUsage > c.targetTokens {
		// 实际使用超出目标，需要扩大窗口
		delta = (actualUsage - c.targetTokens) / 2
		c.targetTokens += delta
	} else if actualUsage < c.targetTokens*80/100 {
		// 实际使用不足目标的80%，缩小窗口以节省Token
		delta = (c.targetTokens - actualUsage) / 4
		delta = -delta
		c.targetTokens += delta
	}
	c.targetTokens = cwc2Clamp(c.targetTokens, c.minTokens, c.maxTokens)
	c.adjustments = append(c.adjustments, delta)
	c.calibrationCount++
	return c.targetTokens
}

// GetCurrentTarget 返回当前的目标Token数。
func (c *ContextWindowCalibratorV2) GetCurrentTarget() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.targetTokens
}

// RecordAdjustment 记录一次手动调整。
func (c *ContextWindowCalibratorV2) RecordAdjustment(delta int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.targetTokens = cwc2Clamp(c.targetTokens+delta, c.minTokens, c.maxTokens)
	c.adjustments = append(c.adjustments, delta)
	c.calibrationCount++
}

// GetStats 返回校准器的统计信息。
func (c *ContextWindowCalibratorV2) GetStats() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	lastAdjustment := 0
	if len(c.adjustments) > 0 {
		lastAdjustment = c.adjustments[len(c.adjustments)-1]
	}
	return map[string]interface{}{
		"targetTokens":     c.targetTokens,
		"minTokens":        c.minTokens,
		"maxTokens":        c.maxTokens,
		"calibrationCount": c.calibrationCount,
		"lastAdjustment":   lastAdjustment,
	}
}

// Reset 重置校准器为初始状态。
func (c *ContextWindowCalibratorV2) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.targetTokens = cwc2Clamp(c.targetTokens, c.minTokens, c.maxTokens)
	c.calibrationCount = 0
	c.adjustments = make([]int, 0)
}

// cwc2Clamp 在 context_window_calibrator.go 中已定义，此处复用。
// func cwc2Clamp(value int, minVal int, maxVal int) int

package agent

import "sync"

// OPT-248: ContextWindowThermalRegulator / 上下文窗口热力调节器
// 根据上下文温度动态调节窗口大小：高温缩小，低温扩大。
type ContextWindowThermalRegulator struct {
	mu              sync.RWMutex
	minSize         int
	maxSize         int
	currentSize     int
	temperature     float64
	adjustments     int
	totalAdjustment int
}

// NewContextWindowThermalRegulator 创建一个新的上下文窗口热力调节器。
// initialSize 会被限制在 [minSize, maxSize] 区间内。
func NewContextWindowThermalRegulator(minSize int, maxSize int, initialSize int) *ContextWindowThermalRegulator {
	if minSize < 0 {
		minSize = 0
	}
	if maxSize < minSize {
		maxSize = minSize
	}
	return &ContextWindowThermalRegulator{
		minSize:     minSize,
		maxSize:     maxSize,
		currentSize: cwtrClampSize(initialSize, minSize, maxSize),
		temperature: 0.5,
	}
}

// SetTemperature 设置温度(0~1)，超出范围会被截断到边界。
func (c *ContextWindowThermalRegulator) SetTemperature(temp float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if temp < 0 {
		temp = 0
	}
	if temp > 1 {
		temp = 1
	}
	c.temperature = temp
}

// Regulate 根据当前温度调节窗口大小（高温缩小，低温扩大），返回新大小。
// 温度0对应maxSize，温度1对应minSize。
func (c *ContextWindowThermalRegulator) Regulate() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.adjustments++

	sizeRange := c.maxSize - c.minSize
	target := int(float64(c.maxSize) - c.temperature*float64(sizeRange))
	target = cwtrClampSize(target, c.minSize, c.maxSize)

	delta := target - c.currentSize
	if delta < 0 {
		delta = -delta
	}
	c.totalAdjustment += delta
	c.currentSize = target
	return c.currentSize
}

// GetTemperature 获取当前温度。
func (c *ContextWindowThermalRegulator) GetTemperature() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.temperature
}

// GetWindowSize 获取当前窗口大小。
func (c *ContextWindowThermalRegulator) GetWindowSize() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.currentSize
}

// GetStats 获取统计信息，包含 minSize、maxSize、currentSize、temperature、adjustments、totalAdjustment。
func (c *ContextWindowThermalRegulator) GetStats() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return map[string]interface{}{
		"minSize":         c.minSize,
		"maxSize":         c.maxSize,
		"currentSize":     c.currentSize,
		"temperature":     c.temperature,
		"adjustments":     c.adjustments,
		"totalAdjustment": c.totalAdjustment,
	}
}

// Reset 重置调节状态，窗口大小恢复为minSize，温度恢复为0.5。
func (c *ContextWindowThermalRegulator) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.currentSize = c.minSize
	c.temperature = 0.5
	c.adjustments = 0
	c.totalAdjustment = 0
}

// cwtrClampSize 将size限制在[minSize, maxSize]区间内（辅助函数）。
func cwtrClampSize(size, minSize, maxSize int) int {
	if size < minSize {
		return minSize
	}
	if size > maxSize {
		return maxSize
	}
	return size
}

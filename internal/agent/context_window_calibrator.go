package agent

import "sync"

// ── OPT-145: ContextWindowCalibrator (上下文窗口校准器) ──
// 动态校准最优上下文窗口大小。根据命中率调整窗口:
//   hitRate > 0.8 -> 增大窗口（+calibrationInterval，受 maxSize 约束）
//   hitRate < 0.4 -> 减小窗口（-calibrationInterval，受 minSize 约束）
//   其他          -> 保持当前窗口大小
// 同时记录每次性能数据到历史列表，供统计分析使用。

// CalibrationRecord 表示一次校准性能记录。
type CalibrationRecord struct {
	WindowSize int
	HitRate    float64
	Efficiency float64
}

// ContextWindowCalibrator 上下文窗口校准器，动态校准最优窗口大小。
type ContextWindowCalibrator struct {
	mu                  sync.RWMutex
	currentSize         int
	minSize             int
	maxSize             int
	totalCalibrations   int
	performanceHistory  []CalibrationRecord
	calibrationInterval int
}

// NewContextWindowCalibrator 创建一个新的上下文窗口校准器。
// minSize 为窗口下限，maxSize 为窗口上限，interval 为每次校准的调整步长。
// 初始窗口大小设为 minSize。
func NewContextWindowCalibrator(minSize int, maxSize int, interval int) *ContextWindowCalibrator {
	return &ContextWindowCalibrator{
		currentSize:         minSize,
		minSize:             minSize,
		maxSize:             maxSize,
		performanceHistory:  make([]CalibrationRecord, 0),
		calibrationInterval: interval,
	}
}

// Calibrate 根据性能记录调整窗口大小并返回调整后的窗口大小。
// hitRate > 0.8 时增大窗口，hitRate < 0.4 时减小窗口，否则保持不变。
// 调整结果受 [minSize, maxSize] 约束。同时记录本次性能数据。
func (c *ContextWindowCalibrator) Calibrate(hitRate float64, efficiency float64) int {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.totalCalibrations++

	c.performanceHistory = append(c.performanceHistory, CalibrationRecord{
		WindowSize: c.currentSize,
		HitRate:    hitRate,
		Efficiency: efficiency,
	})

	if hitRate > 0.8 {
		c.currentSize = cwc2Clamp(c.currentSize+c.calibrationInterval, c.minSize, c.maxSize)
	} else if hitRate < 0.4 {
		c.currentSize = cwc2Clamp(c.currentSize-c.calibrationInterval, c.minSize, c.maxSize)
	}

	return c.currentSize
}

// GetCurrentSize 返回当前窗口大小。
func (c *ContextWindowCalibrator) GetCurrentSize() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.currentSize
}

// RecordPerformance 记录性能数据到历史列表，但不调整窗口大小。
func (c *ContextWindowCalibrator) RecordPerformance(hitRate float64, efficiency float64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.performanceHistory = append(c.performanceHistory, CalibrationRecord{
		WindowSize: c.currentSize,
		HitRate:    hitRate,
		Efficiency: efficiency,
	})
}

// GetStats 返回校准器的统计信息。
// 包含: currentSize, minSize, maxSize, totalCalibrations, avgHitRate, avgEfficiency。
func (c *ContextWindowCalibrator) GetStats() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	avgHitRate, avgEfficiency := cwc2ComputeAverages(c.performanceHistory)
	return map[string]interface{}{
		"currentSize":       c.currentSize,
		"minSize":           c.minSize,
		"maxSize":           c.maxSize,
		"totalCalibrations": c.totalCalibrations,
		"avgHitRate":        avgHitRate,
		"avgEfficiency":     avgEfficiency,
	}
}

// Reset 重置校准器，窗口大小恢复为 minSize，清空历史记录和校准计数。
func (c *ContextWindowCalibrator) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.currentSize = c.minSize
	c.totalCalibrations = 0
	c.performanceHistory = make([]CalibrationRecord, 0)
}

// cwc2Clamp 将值限制在 [min, max] 范围内。
func cwc2Clamp(val, min, max int) int {
	if val < min {
		return min
	}
	if val > max {
		return max
	}
	return val
}

// cwc2ComputeAverages 计算历史记录的平均命中率和平均效率。
// 若历史记录为空则均返回 0。
func cwc2ComputeAverages(history []CalibrationRecord) (float64, float64) {
	if len(history) == 0 {
		return 0, 0
	}
	totalHitRate := 0.0
	totalEfficiency := 0.0
	for _, r := range history {
		totalHitRate += r.HitRate
		totalEfficiency += r.Efficiency
	}
	n := float64(len(history))
	return totalHitRate / n, totalEfficiency / n
}

package agent
import "sync"

// ── OPT-233: ContextWindowCalibratorV3 (上下文窗口校准器V3 / Context Window Calibrator V3) ──
// 根据观察到的实际上下文大小动态校准目标窗口大小。
// 计算观察值与目标值的偏差，将调整量限制在 maxAdjustment 范围内，
// 逐步收敛到最优窗口大小，避免一次性大幅调整导致系统不稳定。

// ContextWindowCalibratorV3 上下文窗口校准器V3
type ContextWindowCalibratorV3 struct {
	mu               sync.RWMutex
	targetSize       int // 目标窗口大小
	actualSize       int // 最近观察到的实际大小
	calibrationCount int // 校准次数
	totalAdjustment  int // 累计调整量（绝对值之和）
	maxAdjustment    int // 单次最大调整量
}

// NewContextWindowCalibratorV3 创建一个新的上下文窗口校准器V3。
// targetSize 为初始目标窗口大小，maxAdjustment 为单次最大调整量。
func NewContextWindowCalibratorV3(targetSize int, maxAdjustment int) *ContextWindowCalibratorV3 {
	return &ContextWindowCalibratorV3{
		targetSize:    targetSize,
		maxAdjustment: maxAdjustment,
	}
}

// Calibrate 根据观察到的实际大小校准目标大小，返回调整后的目标值。
// 计算偏差（观察值 - 目标值），限制在 maxAdjustment 范围内后应用调整。
func (c *ContextWindowCalibratorV3) Calibrate(observedSize int) int {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.actualSize = observedSize
	c.calibrationCount++

	// 计算偏差：观察值与目标值的差值
	delta := observedSize - c.targetSize

	// 限制单次调整量
	clampedDelta := cwcv3ClampAdjustment(delta, c.maxAdjustment)

	// 应用调整到目标值
	c.targetSize += clampedDelta

	// 累计总调整量（绝对值）
	if clampedDelta < 0 {
		c.totalAdjustment += -clampedDelta
	} else {
		c.totalAdjustment += clampedDelta
	}

	return c.targetSize
}

// GetCalibrationDelta 获取当前实际大小与目标大小的差距（actualSize - targetSize）。
func (c *ContextWindowCalibratorV3) GetCalibrationDelta() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.actualSize - c.targetSize
}

// GetAdjustmentHistory 获取累计总调整量（绝对值之和）。
func (c *ContextWindowCalibratorV3) GetAdjustmentHistory() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.totalAdjustment
}

// GetStats 返回校准器的统计信息。
// 包含 targetSize、actualSize、calibrationCount、totalAdjustment、maxAdjustment。
func (c *ContextWindowCalibratorV3) GetStats() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return map[string]interface{}{
		"targetSize":       c.targetSize,
		"actualSize":       c.actualSize,
		"calibrationCount": c.calibrationCount,
		"totalAdjustment":  c.totalAdjustment,
		"maxAdjustment":    c.maxAdjustment,
	}
}

// Reset 重置校准器的统计信息（不重置 targetSize 和 maxAdjustment 配置）。
func (c *ContextWindowCalibratorV3) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.actualSize = 0
	c.calibrationCount = 0
	c.totalAdjustment = 0
}

// cwcv3ClampAdjustment 将调整量限制在 [-maxAdjustment, maxAdjustment] 范围内（辅助函数）。
// 若 maxAdjustment <= 0，返回 0（不允许调整）。
func cwcv3ClampAdjustment(delta int, maxAdjustment int) int {
	if maxAdjustment <= 0 {
		return 0
	}
	if delta > maxAdjustment {
		return maxAdjustment
	}
	if delta < -maxAdjustment {
		return -maxAdjustment
	}
	return delta
}

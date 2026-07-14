package agent

import "sync"

// ── OPT-198: ContextWindowPredictorV2 (上下文窗口预测器 V2) ──
// 预测上下文窗口的未来使用趋势，基于历史数据使用移动平均进行预测。
// 通过 Record 记录实际使用量，通过 Predict 预测下一步使用量，
// 并在后续 Record 时自动比对预测与实际值的偏差以计算准确率。
//
// 注意：本类型与 window_predictor.go 中的 ContextWindowPredictor (OPT-27)
// 功能不同，为避免同包命名冲突，采用 V2 后缀。

// ContextWindowPredictorV2 上下文窗口预测器
type ContextWindowPredictorV2 struct {
	mu                  sync.RWMutex
	history             []int
	maxHistorySize      int
	predictions         int
	accuratePredictions int
	windowSize          int
	// 内部字段：用于准确率计算
	lastPrediction int  // 上一次预测值
	hasPrediction  bool // 是否存在待验证的预测
}

// NewContextWindowPredictorV2 创建一个新的上下文窗口预测器。
// windowSize 为上下文窗口大小，maxHistorySize 为历史记录最大长度。
func NewContextWindowPredictorV2(windowSize int, maxHistorySize int) *ContextWindowPredictorV2 {
	return &ContextWindowPredictorV2{
		windowSize:     windowSize,
		maxHistorySize: maxHistorySize,
		history:        make([]int, 0, maxHistorySize),
	}
}

// Record 记录当前窗口使用量。
// 若存在之前的预测，将其与实际值比对：偏差在 windowSize 的 10% 以内
// 视为准确预测，递增 accuratePredictions。随后将使用量追加到历史记录，
// 若超过 maxHistorySize 则裁剪最旧的数据。
func (c *ContextWindowPredictorV2) Record(usage int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 若有之前的预测，比较准确度
	if c.hasPrediction {
		diff := usage - c.lastPrediction
		if diff < 0 {
			diff = -diff
		}
		threshold := c.windowSize / 10
		if threshold <= 0 {
			threshold = 1
		}
		if diff <= threshold {
			c.accuratePredictions++
		}
	}

	// 添加到历史记录
	c.history = append(c.history, usage)
	if len(c.history) > c.maxHistorySize {
		c.history = c.history[len(c.history)-c.maxHistorySize:]
	}

	// 重置预测标记，等待下一次 Predict
	c.hasPrediction = false
}

// Predict 基于历史数据预测下一个窗口使用量（移动平均）。
// 递增 predictions 计数并记录预测值供后续准确率计算。
func (c *ContextWindowPredictorV2) Predict() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	prediction := cwpMovingAverage(c.history)
	c.lastPrediction = prediction
	c.hasPrediction = true
	c.predictions++
	return prediction
}

// GetAccuracy 返回预测准确率（accuratePredictions / predictions）。
// 若 predictions 为 0 则返回 0。
func (c *ContextWindowPredictorV2) GetAccuracy() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.predictions == 0 {
		return 0
	}
	return float64(c.accuratePredictions) / float64(c.predictions)
}

// GetStats 返回预测器的统计信息。
// 包含: windowSize, historySize, predictions, accuratePredictions, accuracy。
func (c *ContextWindowPredictorV2) GetStats() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var accuracy float64
	if c.predictions > 0 {
		accuracy = float64(c.accuratePredictions) / float64(c.predictions)
	}

	return map[string]interface{}{
		"windowSize":          c.windowSize,
		"historySize":         len(c.history),
		"predictions":         c.predictions,
		"accuratePredictions": c.accuratePredictions,
		"accuracy":            accuracy,
	}
}

// Reset 重置预测器，清空历史记录与统计信息。
func (c *ContextWindowPredictorV2) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.history = make([]int, 0, c.maxHistorySize)
	c.predictions = 0
	c.accuratePredictions = 0
	c.lastPrediction = 0
	c.hasPrediction = false
}

// ---------------------------------------------------------------------------
// 辅助函数（以 cwp 为前缀，避免命名冲突）
// ---------------------------------------------------------------------------

// cwpMovingAverage 计算历史数据的移动平均值。
// 若 data 为空返回 0。
func cwpMovingAverage(data []int) int {
	if len(data) == 0 {
		return 0
	}
	sum := 0
	for _, v := range data {
		sum += v
	}
	return sum / len(data)
}

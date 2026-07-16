package agent

import "sync"

// ── OPT-258: ContextWindowProactiveAdjuster (上下文窗口主动调整器) ──
// 根据近期观察到的实际上下文大小趋势，主动调整上下文窗口建议大小，
// 避免频繁被动扩缩容。trend: 1=增长, -1=收缩, 0=稳定。

// ContextWindowProactiveAdjuster 上下文窗口主动调整器。
type ContextWindowProactiveAdjuster struct {
	mu               sync.RWMutex
	currentSize      int   // 当前建议的上下文窗口大小
	targetSize       int   // 目标上下文窗口大小
	trend            int   // 当前趋势：1=增长, -1=收缩, 0=稳定
	adjustments      int   // 累计主动调整次数
	predictionWindow int   // 趋势预测窗口（保留的最近观察数量）
	initialSize      int   // 初始大小（用于 Reset 还原）
	history          []int // 最近观察到的实际上下文大小
}

// NewContextWindowProactiveAdjuster 创建一个新的上下文窗口主动调整器。
// initialSize 为初始建议大小，targetSize 为目标大小，predictionWindow 为趋势预测窗口。
// predictionWindow < 1 时视为 1。
func NewContextWindowProactiveAdjuster(initialSize int, targetSize int, predictionWindow int) *ContextWindowProactiveAdjuster {
	if predictionWindow < 1 {
		predictionWindow = 1
	}
	return &ContextWindowProactiveAdjuster{
		currentSize:      initialSize,
		targetSize:       targetSize,
		trend:            0,
		adjustments:      0,
		predictionWindow: predictionWindow,
		initialSize:      initialSize,
		history:          make([]int, 0, predictionWindow),
	}
}

// Observe 观察实际上下文大小并主动调整，返回建议大小。
// 根据预测窗口内的趋势，在目标大小基础上主动预留或回收空间。
func (a *ContextWindowProactiveAdjuster) Observe(actualSize int) int {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.history = append(a.history, actualSize)
	if len(a.history) > a.predictionWindow {
		a.history = a.history[len(a.history)-a.predictionWindow:]
	}

	a.trend = cwpaComputeTrend(a.history)

	suggested := a.currentSize
	switch a.trend {
	case 1:
		// 增长趋势：在目标之上预留约 10% 余量，减少频繁扩容。
		suggested = a.targetSize + a.targetSize/10
	case -1:
		// 收缩趋势：在目标之下回收约 10% 空间。
		suggested = a.targetSize - a.targetSize/10
		if suggested < 0 {
			suggested = 0
		}
	default:
		// 稳定趋势：贴合目标大小。
		suggested = a.targetSize
	}

	if suggested != a.currentSize {
		a.currentSize = suggested
		a.adjustments++
	}
	return a.currentSize
}

// GetTrend 返回当前趋势（1=增长, -1=收缩, 0=稳定）。
func (a *ContextWindowProactiveAdjuster) GetTrend() int {
	a.mu.RLock()
	defer a.mu.RUnlock()

	return a.trend
}

// GetCurrentSize 返回当前建议的上下文窗口大小。
func (a *ContextWindowProactiveAdjuster) GetCurrentSize() int {
	a.mu.RLock()
	defer a.mu.RUnlock()

	return a.currentSize
}

// GetStats 返回调整器的统计信息。
// 包含: currentSize, targetSize, trend, adjustments, predictionWindow。
func (a *ContextWindowProactiveAdjuster) GetStats() map[string]interface{} {
	a.mu.RLock()
	defer a.mu.RUnlock()

	return map[string]interface{}{
		"currentSize":      a.currentSize,
		"targetSize":       a.targetSize,
		"trend":            a.trend,
		"adjustments":      a.adjustments,
		"predictionWindow": a.predictionWindow,
	}
}

// Reset 重置调整器，恢复到初始状态，保留 targetSize 与 predictionWindow 配置。
func (a *ContextWindowProactiveAdjuster) Reset() {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.currentSize = a.initialSize
	a.trend = 0
	a.adjustments = 0
	a.history = make([]int, 0, a.predictionWindow)
}

// ---------------------------------------------------------------------------
// 辅助函数（以 cwpa 为前缀，避免命名冲突）
// ---------------------------------------------------------------------------

// cwpaComputeTrend 根据历史观察计算趋势。
// 比较窗口内最早与最近的观察值：最近更大返回 1（增长），更小返回 -1（收缩），相等返回 0（稳定）。
// 观察数不足 2 个时视为稳定。
func cwpaComputeTrend(history []int) int {
	n := len(history)
	if n < 2 {
		return 0
	}
	first := history[0]
	last := history[n-1]
	if last > first {
		return 1
	}
	if last < first {
		return -1
	}
	return 0
}

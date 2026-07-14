package agent

import "sync"

// ── OPT-110: AdaptiveBatchOptimizer (自适应批处理优化器) ──
// 优化批量请求的 token 效率，根据反馈动态调整最优批次大小。
//
// 原理：维护一个当前批次缓冲区，当其大小达到 optimalBatchSize 时
// 触发 flush。Flush 后记录历史，外部可根据实际 token 反馈调用
// AdjustBatchSize 动态调优：正向反馈增大批次，负向反馈缩小批次，
// 范围限制在 [1, 100]。
//
// 效果：在吞吐与 token 开销之间自动寻找平衡点，避免批次过大导致
// 超长 prompt 或批次过小导致请求次数过多。

// AdaptiveBatchOptimizer 自适应批处理优化器。
type AdaptiveBatchOptimizer struct {
	mu               sync.RWMutex
	totalBatches     int
	totalItems       int
	totalTokens      int
	optimalBatchSize int
	batchSizeHistory []int
	currentBatch     []string
}

// NewAdaptiveBatchOptimizer 创建新的自适应批处理优化器。
// initialSize 为初始最优批次大小。
func NewAdaptiveBatchOptimizer(initialSize int) *AdaptiveBatchOptimizer {
	return &AdaptiveBatchOptimizer{
		optimalBatchSize: initialSize,
		currentBatch:     make([]string, 0),
		batchSizeHistory: make([]int, 0),
	}
}

// AddItem 将一个条目添加到当前批次。
func (o *AdaptiveBatchOptimizer) AddItem(item string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.currentBatch = append(o.currentBatch, item)
	o.totalItems++
	o.totalTokens += aboEstimateTokens(item)
}

// ShouldFlush 判断当前批次是否应该刷新。
// 当当前批次大小 >= optimalBatchSize 时返回 true。
func (o *AdaptiveBatchOptimizer) ShouldFlush() bool {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return len(o.currentBatch) >= o.optimalBatchSize
}

// Flush 返回当前批次并清空缓冲区，同时记录批次大小到历史。
// 若当前批次为空则返回 nil。
func (o *AdaptiveBatchOptimizer) Flush() []string {
	o.mu.Lock()
	defer o.mu.Unlock()

	if len(o.currentBatch) == 0 {
		return nil
	}

	result := o.currentBatch
	o.batchSizeHistory = append(o.batchSizeHistory, len(result))
	o.totalBatches++
	o.currentBatch = make([]string, 0)
	return result
}

// AdjustBatchSize 根据 token 反馈调整最优批次大小。
// tokenFeedback > 0 时增大批次，< 0 时缩小批次，范围为 [1, 100]。
func (o *AdaptiveBatchOptimizer) AdjustBatchSize(tokenFeedback int) {
	o.mu.Lock()
	defer o.mu.Unlock()

	delta := tokenFeedback / 100
	if delta == 0 && tokenFeedback > 0 {
		delta = 1
	}
	if delta == 0 && tokenFeedback < 0 {
		delta = -1
	}

	o.optimalBatchSize += delta
	if o.optimalBatchSize < 1 {
		o.optimalBatchSize = 1
	}
	if o.optimalBatchSize > 100 {
		o.optimalBatchSize = 100
	}
}

// GetStats 返回优化器统计信息，包括 totalBatches、totalItems、
// totalTokens、optimalBatchSize 和 currentBatchSize。
func (o *AdaptiveBatchOptimizer) GetStats() map[string]interface{} {
	o.mu.RLock()
	defer o.mu.RUnlock()

	return map[string]interface{}{
		"totalBatches":     o.totalBatches,
		"totalItems":       o.totalItems,
		"totalTokens":      o.totalTokens,
		"optimalBatchSize": o.optimalBatchSize,
		"currentBatchSize": len(o.currentBatch),
	}
}

// Reset 重置优化器状态，清空当前批次、历史和计数，
// 但保留 optimalBatchSize 不变。
func (o *AdaptiveBatchOptimizer) Reset() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.totalBatches = 0
	o.totalItems = 0
	o.totalTokens = 0
	o.batchSizeHistory = make([]int, 0)
	o.currentBatch = make([]string, 0)
}

// aboEstimateTokens 粗略估算字符串的 token 数（约 4 字符/token）。
func aboEstimateTokens(s string) int {
	if len(s) == 0 {
		return 0
	}
	return (len(s) + 3) / 4
}

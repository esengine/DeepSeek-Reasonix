package agent

import "sync"

// OPT-199: TokenAwareBatchOptimizer / Token感知批次优化器
// 优化批次大小以最小化Token浪费，根据item数量和token预算计算最优批次大小。

// TokenAwareBatchOptimizer 是Token感知批次优化器。
type TokenAwareBatchOptimizer struct {
	mu                  sync.RWMutex
	optimalBatchSize    int
	minBatchSize        int
	maxBatchSize        int
	batchCount          int
	totalItemsProcessed int
}

// NewTokenAwareBatchOptimizer 创建一个新的TokenAwareBatchOptimizer实例。
// optimalBatchSize 初始化为 (minSize + maxSize) / 2
func NewTokenAwareBatchOptimizer(minSize int, maxSize int) *TokenAwareBatchOptimizer {
	optimal := (minSize + maxSize) / 2
	return &TokenAwareBatchOptimizer{
		optimalBatchSize:    optimal,
		minBatchSize:        minSize,
		maxBatchSize:        maxSize,
		batchCount:          0,
		totalItemsProcessed: 0,
	}
}

// Optimize 根据item数量和token预算计算最优批次大小，并更新内部最优值。
func (t *TokenAwareBatchOptimizer) Optimize(itemCount int, tokenBudget int) int {
	t.mu.Lock()
	defer t.mu.Unlock()

	optimized := taboComputeOptimal(itemCount, tokenBudget, t.minBatchSize, t.maxBatchSize)
	t.optimalBatchSize = optimized
	return optimized
}

// GetOptimalBatchSize 返回当前的最优批次大小。
func (t *TokenAwareBatchOptimizer) GetOptimalBatchSize() int {
	t.mu.RLock()
	defer t.mu.RUnlock()

	return t.optimalBatchSize
}

// RecordBatch 记录一次批次处理，更新统计信息。
func (t *TokenAwareBatchOptimizer) RecordBatch(itemCount int) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.batchCount++
	t.totalItemsProcessed += itemCount
}

// GetStats 返回优化器的统计信息。
func (t *TokenAwareBatchOptimizer) GetStats() map[string]interface{} {
	t.mu.RLock()
	defer t.mu.RUnlock()

	return map[string]interface{}{
		"optimalBatchSize":    t.optimalBatchSize,
		"minBatchSize":        t.minBatchSize,
		"maxBatchSize":        t.maxBatchSize,
		"batchCount":          t.batchCount,
		"totalItemsProcessed": t.totalItemsProcessed,
		"avgBatchSize":        taboComputeAvg(t.totalItemsProcessed, t.batchCount),
	}
}

// Reset 重置优化器为初始状态。
func (t *TokenAwareBatchOptimizer) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.optimalBatchSize = (t.minBatchSize + t.maxBatchSize) / 2
	t.batchCount = 0
	t.totalItemsProcessed = 0
}

// taboComputeOptimal 根据item数量和token预算计算最优批次大小。
// 策略：基于token预算和预估每item的token开销，计算可处理的批次大小，
// 并限制在[minSize, maxSize]范围内。
func taboComputeOptimal(itemCount int, tokenBudget int, minSize int, maxSize int) int {
	if itemCount <= 0 || tokenBudget <= 0 {
		return minSize
	}
	// 估算每item平均token开销（假设每item约100 token）
	avgTokenPerItem := 100
	if avgTokenPerItem == 0 {
		return minSize
	}
	// 基于预算计算可处理的item数
	affordable := tokenBudget / avgTokenPerItem
	if affordable <= 0 {
		return minSize
	}
	// 不超过总item数
	if affordable > itemCount {
		affordable = itemCount
	}
	// 限制在[min, max]范围
	if affordable < minSize {
		return minSize
	}
	if affordable > maxSize {
		return maxSize
	}
	return affordable
}

// taboComputeAvg 计算平均批次大小。
func taboComputeAvg(totalItems int, batchCount int) float64 {
	if batchCount == 0 {
		return 0.0
	}
	return float64(totalItems) / float64(batchCount)
}

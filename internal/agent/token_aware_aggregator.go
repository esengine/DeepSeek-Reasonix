package agent

import "sync"

// ── OPT-166: TokenAwareAggregator (Token 感知聚合器) ──
// 将多个小请求聚合为批量请求以减少开销。
// 通过按批次大小和 token 预算双阈值控制，自动判断何时刷新批次。

// AggregationItem 聚合项，包含唯一标识、内容和估算的 token 数
type AggregationItem struct {
	ID              string
	Content         string
	EstimatedTokens int
}

// TokenAwareAggregator Token 感知聚合器，将多个小请求聚合为批量请求
type TokenAwareAggregator struct {
	mu              sync.RWMutex
	maxBatchSize    int
	maxBatchTokens  int
	pending         []AggregationItem
	flushedBatches  int
	totalAggregated int
}

// NewTokenAwareAggregator 创建 Token 感知聚合器
func NewTokenAwareAggregator(maxBatchSize int, maxBatchTokens int) *TokenAwareAggregator {
	return &TokenAwareAggregator{
		maxBatchSize:   maxBatchSize,
		maxBatchTokens: maxBatchTokens,
		pending:        make([]AggregationItem, 0, maxBatchSize),
	}
}

// Add 添加项到待处理列表，返回是否应立即刷新
func (a *TokenAwareAggregator) Add(item AggregationItem) bool {
	a.mu.Lock()
	defer a.mu.Unlock()

	// 若未提供 token 估算值，则自动计算
	if item.EstimatedTokens <= 0 {
		item.EstimatedTokens = taagEstimateTokens(item.Content)
	}
	a.pending = append(a.pending, item)
	a.totalAggregated++

	return a.shouldFlushLocked()
}

// Flush 刷新待处理列表，返回当前批次并清空
func (a *TokenAwareAggregator) Flush() []AggregationItem {
	a.mu.Lock()
	defer a.mu.Unlock()

	batch := a.pending
	a.pending = make([]AggregationItem, 0, a.maxBatchSize)
	a.flushedBatches++
	return batch
}

// Peek 查看待处理列表（不清空）
func (a *TokenAwareAggregator) Peek() []AggregationItem {
	a.mu.RLock()
	defer a.mu.RUnlock()

	result := make([]AggregationItem, len(a.pending))
	copy(result, a.pending)
	return result
}

// ShouldFlush 检查是否应刷新（达到 maxBatchSize 或 maxBatchTokens）
func (a *TokenAwareAggregator) ShouldFlush() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()

	return a.shouldFlushLocked()
}

// shouldFlushLocked 在已持有锁的情况下检查是否应刷新
func (a *TokenAwareAggregator) shouldFlushLocked() bool {
	if len(a.pending) >= a.maxBatchSize {
		return true
	}
	totalTokens := 0
	for _, item := range a.pending {
		totalTokens += item.EstimatedTokens
	}
	return totalTokens >= a.maxBatchTokens
}

// GetStats 返回聚合器统计信息
func (a *TokenAwareAggregator) GetStats() map[string]interface{} {
	a.mu.RLock()
	defer a.mu.RUnlock()

	return map[string]interface{}{
		"maxBatchSize":    a.maxBatchSize,
		"maxBatchTokens":  a.maxBatchTokens,
		"pendingCount":    len(a.pending),
		"flushedBatches":  a.flushedBatches,
		"totalAggregated": a.totalAggregated,
	}
}

// Reset 重置聚合器
func (a *TokenAwareAggregator) Reset() {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.pending = make([]AggregationItem, 0, a.maxBatchSize)
	a.flushedBatches = 0
	a.totalAggregated = 0
}

// taagEstimateTokens 估算字符串的 token 数量（近似值：字符数 / 4）
func taagEstimateTokens(content string) int {
	return len(content) / 4
}

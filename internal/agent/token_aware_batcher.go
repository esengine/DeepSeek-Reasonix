package agent

import "sync"

// ── OPT-137: TokenAwareBatcher (Token 感知批处理器) ──
// 按 token 预算批量处理消息。向当前批次添加项目时，若加入后不超过
// token 预算则保留在批次中；否则需先 Flush 再添加。Flush 将当前
// 批次作为一个处理单元返回，并累计批次/token 统计。
//
// Token 估算: len(content) / 4（近似）。

// BatchItem 批处理项目。
type BatchItem struct {
	Content string
	Tokens  int
}

// TokenAwareBatcher Token 感知批处理器，按 token 预算批量处理消息。
type TokenAwareBatcher struct {
	mu                   sync.RWMutex
	currentBatch         []BatchItem
	currentTokenCount    int
	tokenBudget          int
	totalBatches         int
	totalItemsProcessed  int
	totalTokensProcessed int
}

// NewTokenAwareBatcher 创建一个新的 Token 感知批处理器。
func NewTokenAwareBatcher(tokenBudget int) *TokenAwareBatcher {
	return &TokenAwareBatcher{
		tokenBudget: tokenBudget,
	}
}

// AddItem 添加项目到当前批次。
// 若加入后不超过 token 预算，则添加并返回 true；
// 否则不添加并返回 false（调用方应先 Flush 再重试）。
func (b *TokenAwareBatcher) AddItem(content string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	tokens := tabEstimateTokens(content)
	if b.currentTokenCount+tokens > b.tokenBudget {
		return false
	}
	b.currentBatch = append(b.currentBatch, BatchItem{Content: content, Tokens: tokens})
	b.currentTokenCount += tokens
	return true
}

// Flush 刷新当前批次，返回项目列表并清空当前批次。
// 若当前批次非空，则递增 totalBatches，并累计 totalItemsProcessed
// 与 totalTokensProcessed。
func (b *TokenAwareBatcher) Flush() []BatchItem {
	b.mu.Lock()
	defer b.mu.Unlock()

	batch := b.currentBatch
	if len(batch) > 0 {
		b.totalBatches++
		b.totalItemsProcessed += len(batch)
		b.totalTokensProcessed += b.currentTokenCount
	}
	b.currentBatch = nil
	b.currentTokenCount = 0
	return batch
}

// ShouldFlush 返回当前 token 数是否达到或超过预算。
func (b *TokenAwareBatcher) ShouldFlush() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.currentTokenCount >= b.tokenBudget
}

// GetCurrentTokenCount 返回当前批次的 token 数。
func (b *TokenAwareBatcher) GetCurrentTokenCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.currentTokenCount
}

// GetStats 返回批处理器的统计信息。
// 包含 totalBatches、totalItemsProcessed、totalTokensProcessed、
// avgBatchSize 与 avgBatchTokens。
func (b *TokenAwareBatcher) GetStats() map[string]interface{} {
	b.mu.RLock()
	defer b.mu.RUnlock()

	avgBatchSize := 0.0
	avgBatchTokens := 0.0
	if b.totalBatches > 0 {
		avgBatchSize = float64(b.totalItemsProcessed) / float64(b.totalBatches)
		avgBatchTokens = float64(b.totalTokensProcessed) / float64(b.totalBatches)
	}
	return map[string]interface{}{
		"totalBatches":         b.totalBatches,
		"totalItemsProcessed":  b.totalItemsProcessed,
		"totalTokensProcessed": b.totalTokensProcessed,
		"avgBatchSize":         avgBatchSize,
		"avgBatchTokens":       avgBatchTokens,
	}
}

// Reset 重置批处理器的当前批次与累计统计。
func (b *TokenAwareBatcher) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.currentBatch = nil
	b.currentTokenCount = 0
	b.totalBatches = 0
	b.totalItemsProcessed = 0
	b.totalTokensProcessed = 0
}

// ---------------------------------------------------------------------------
// 辅助函数 (tab 前缀，避免与 tap 冲突)
// ---------------------------------------------------------------------------

// tabEstimateTokens 估算内容的 token 数: len(content) / 4。
func tabEstimateTokens(content string) int {
	return len(content) / 4
}

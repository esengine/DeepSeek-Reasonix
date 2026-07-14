package agent
import "sync"

// OPT-171: TokenAwareBuffer — Token感知缓冲区
// TokenAwareBuffer buffers tokens to support batch flushing.
// It accumulates content until the estimated token count reaches capacity,
// at which point a flush is triggered automatically.
type TokenAwareBuffer struct {
	mu             sync.RWMutex
	capacity       int      // 缓冲区容量（以token计）buffer capacity in tokens
	currentTokens  int      // 当前缓冲的token数 currently buffered tokens
	flushCount     int      // 刷新次数 number of flushes performed
	totalBuffered  int      // 累计缓冲的token总数 total tokens ever buffered
	items          []string // 缓冲的内容项 buffered content items
}

// NewTokenAwareBuffer creates a new TokenAwareBuffer with the given token capacity.
// NewTokenAwareBuffer 使用给定的token容量创建新的TokenAwareBuffer。
func NewTokenAwareBuffer(capacity int) *TokenAwareBuffer {
	return &TokenAwareBuffer{
		capacity:      capacity,
		currentTokens: 0,
		flushCount:    0,
		totalBuffered: 0,
		items:         make([]string, 0),
	}
}

// Write writes content into the buffer and returns true if a flush should be triggered.
// Write 将内容写入缓冲区，返回是否应触发刷新。
func (t *TokenAwareBuffer) Write(content string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	estimated := tabEstimateTokens(content)
	t.items = append(t.items, content)
	t.currentTokens += estimated
	t.totalBuffered += estimated

	return t.currentTokens >= t.capacity
}

// Flush flushes the buffer, returning all content joined and clearing the buffer.
// Flush 刷新缓冲区，返回所有拼接内容并清空缓冲区。
func (t *TokenAwareBuffer) Flush() string {
	t.mu.Lock()
	defer t.mu.Unlock()

	result := ""
	for _, item := range t.items {
		result += item
	}

	t.items = make([]string, 0)
	t.currentTokens = 0
	t.flushCount++

	return result
}

// GetCurrentTokens returns the current number of buffered tokens.
// GetCurrentTokens 返回当前缓冲的token数。
func (t *TokenAwareBuffer) GetCurrentTokens() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.currentTokens
}

// IsFull returns true if the buffer has reached or exceeded its capacity.
// IsFull 返回缓冲区是否已达到或超过容量。
func (t *TokenAwareBuffer) IsFull() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.currentTokens >= t.capacity
}

// GetStats returns statistics about the buffer.
// GetStats 返回缓冲区的统计信息。
func (t *TokenAwareBuffer) GetStats() map[string]interface{} {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return map[string]interface{}{
		"capacity":      t.capacity,
		"currentTokens": t.currentTokens,
		"flushCount":    t.flushCount,
		"totalBuffered": t.totalBuffered,
	}
}

// Reset resets the buffer to its initial state (preserving capacity).
// Reset 将缓冲区重置为初始状态（保留容量配置）。
func (t *TokenAwareBuffer) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.currentTokens = 0
	t.flushCount = 0
	t.totalBuffered = 0
	t.items = make([]string, 0)
}

// tabEstimateTokens is defined in token_aware_batcher.go (tab prefix, len/4).
// tabEstimateTokens 定义在 token_aware_batcher.go 中（tab前缀，len/4）。

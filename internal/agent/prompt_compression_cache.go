package agent

import "sync"

// ── OPT-155: PromptCompressionCache (提示压缩缓存) ──
// 缓存压缩后的提示版本以避免重复压缩。通过内容哈希索引压缩结果，
// 当相同提示再次出现时直接返回缓存的压缩版本，跳过耗时的压缩计算。
//
// 原理：提示压缩（如去除冗余、摘要化、模板化）是计算密集型操作。
// 在多轮对话中，许多提示片段会重复出现（如系统提示、工具描述）。
// 通过以内容哈希为键缓存压缩结果，可以在命中时直接返回缓存，
// 避免重复压缩。当缓存条目超过上限时，采用随机淘汰策略移除旧条目。
//
// 效果：减少重复压缩的计算开销，通过命中率与节省 token 数统计
// 量化缓存效果，为压缩策略优化提供反馈。

// PromptCompressionCache 提示压缩缓存
type PromptCompressionCache struct {
	mu        sync.RWMutex
	entries   map[string]string // hash -> compressed prompt
	metadata  map[string]int    // hash -> tokensSaved
	maxEntries int
	hits      int
	misses    int
}

// NewPromptCompressionCache 创建提示压缩缓存。
// maxEntries 指定最大缓存条目数，若 <= 0 则默认 1000。
func NewPromptCompressionCache(maxEntries int) *PromptCompressionCache {
	if maxEntries <= 0 {
		maxEntries = 1000
	}
	return &PromptCompressionCache{
		entries:    make(map[string]string),
		metadata:   make(map[string]int),
		maxEntries: maxEntries,
	}
}

// Get 获取缓存的压缩结果。
// 若缓存命中，递增 hits 计数并返回 (compressed, true)；
// 若缓存未命中，递增 misses 计数并返回 ("", false)。
func (c *PromptCompressionCache) Get(hash string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	compressed, ok := c.entries[hash]
	if ok {
		c.hits++
		return compressed, true
	}

	c.misses++
	return "", false
}

// Put 存储压缩结果。
// hash 为内容哈希，compressed 为压缩后的文本，tokensSaved 为本次压缩节省的 token 数。
// 若条目已存在则更新。若缓存已满（达到 maxEntries），随机淘汰一个旧条目后再插入。
func (c *PromptCompressionCache) Put(hash string, compressed string, tokensSaved int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 若条目已存在，直接更新
	if _, exists := c.entries[hash]; exists {
		c.entries[hash] = compressed
		c.metadata[hash] = tokensSaved
		return
	}

	// 缓存已满时随机淘汰（Go map 迭代顺序随机）
	if len(c.entries) >= c.maxEntries && c.maxEntries > 0 {
		for k := range c.entries {
			delete(c.entries, k)
			delete(c.metadata, k)
			break
		}
	}

	c.entries[hash] = compressed
	c.metadata[hash] = tokensSaved
}

// Invalidate 失效特定条目，从缓存中移除指定哈希的压缩结果。
func (c *PromptCompressionCache) Invalidate(hash string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.entries, hash)
	delete(c.metadata, hash)
}

// GetHitRate 返回缓存命中率: hits / (hits + misses)。
// 若 hits + misses 为 0 则返回 0。
func (c *PromptCompressionCache) GetHitRate() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return pccComputeHitRate(c.hits, c.misses)
}

// GetStats 返回缓存的统计信息，包括 entries、maxEntries、hits、misses、hitRate、totalTokensSaved。
func (c *PromptCompressionCache) GetStats() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return map[string]interface{}{
		"entries":          len(c.entries),
		"maxEntries":       c.maxEntries,
		"hits":             c.hits,
		"misses":           c.misses,
		"hitRate":          pccComputeHitRate(c.hits, c.misses),
		"totalTokensSaved": pccSumTokensSaved(c.metadata),
	}
}

// Reset 重置缓存，清除所有条目与统计信息。
func (c *PromptCompressionCache) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries = make(map[string]string)
	c.metadata = make(map[string]int)
	c.hits = 0
	c.misses = 0
}

// ---------------------------------------------------------------------------
// 辅助函数（以 pcc 为前缀，避免命名冲突）
// ---------------------------------------------------------------------------

// pccComputeHitRate 计算命中率: hits / (hits + misses)。
// 若 hits + misses 为 0，返回 0。
func pccComputeHitRate(hits, misses int) float64 {
	total := hits + misses
	if total == 0 {
		return 0
	}
	return float64(hits) / float64(total)
}

// pccSumTokensSaved 汇总所有缓存条目节省的 token 总数。
func pccSumTokensSaved(metadata map[string]int) int {
	total := 0
	for _, saved := range metadata {
		total += saved
	}
	return total
}

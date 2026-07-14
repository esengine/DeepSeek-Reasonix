package agent

import "sync"

// ── OPT-119: PromptSegmentCacheV2 (Prompt 片段缓存 V2) ──
// 支持细粒度片段级缓存，通过 key 索引存储和检索 prompt 片段，
// 跟踪命中/未命中统计并估算节省的 token 数量。
//
// 原理：将 prompt 拆分为多个可独立缓存的片段，每片通过唯一 key 索引。
// 当相同的片段再次请求时直接从缓存返回，避免重复生成或传输。
// 片段大小受 maxSegmentSize 限制，超出部分截断以控制内存占用。
//
// 效果：减少重复 prompt 片段的 token 开销，提升缓存命中率和响应速度。

// PromptSegmentCacheV2 Prompt 片段缓存 V2
type PromptSegmentCacheV2 struct {
	mu               sync.RWMutex
	segments         map[string]string
	hits             int
	misses           int
	totalSegments    int
	totalSavedTokens int
	maxSegmentSize   int
}

// NewPromptSegmentCacheV2 创建 Prompt 片段缓存 V2。
// maxSegmentSize 指定单个片段的最大字符数，超过该值的片段将被截断。
func NewPromptSegmentCacheV2(maxSegmentSize int) *PromptSegmentCacheV2 {
	return &PromptSegmentCacheV2{
		segments:       make(map[string]string),
		maxSegmentSize: maxSegmentSize,
	}
}

// GetSegment 获取缓存的片段。
// 若命中则递增 hits 计数并累加节省的 token 估算；若未命中则递增 misses 计数。
// 返回片段内容和是否命中。
func (c *PromptSegmentCacheV2) GetSegment(key string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	content, ok := c.segments[key]
	if ok {
		c.hits++
		c.totalSavedTokens += pscEstimateTokens(content)
		return content, true
	}
	c.misses++
	return "", false
}

// PutSegment 存储片段。
// 若内容超过 maxSegmentSize 则截断到 maxSegmentSize。
// 新增片段递增 totalSegments 计数，已存在的 key 则更新内容。
func (c *PromptSegmentCacheV2) PutSegment(key string, content string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.maxSegmentSize > 0 && len(content) > c.maxSegmentSize {
		content = content[:c.maxSegmentSize]
	}

	if _, exists := c.segments[key]; !exists {
		c.totalSegments++
	}
	c.segments[key] = content
}

// Invalidate 使指定片段失效，从缓存中移除。
func (c *PromptSegmentCacheV2) Invalidate(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.segments, key)
}

// GetHitRate 返回缓存命中率。
// 命中率 = hits / (hits + misses)，若分母为 0 则返回 0。
func (c *PromptSegmentCacheV2) GetHitRate() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	total := c.hits + c.misses
	if total == 0 {
		return 0
	}
	return float64(c.hits) / float64(total)
}

// GetStats 返回缓存的统计信息，包括 hits、misses、hitRate、
// totalSegments、totalSavedTokens 和 activeSegments。
func (c *PromptSegmentCacheV2) GetStats() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	total := c.hits + c.misses
	var hitRate float64
	if total > 0 {
		hitRate = float64(c.hits) / float64(total)
	}

	return map[string]interface{}{
		"hits":             c.hits,
		"misses":           c.misses,
		"hitRate":          hitRate,
		"totalSegments":    c.totalSegments,
		"totalSavedTokens": c.totalSavedTokens,
		"activeSegments":   len(c.segments),
	}
}

// Reset 重置缓存，清除所有片段与统计。
func (c *PromptSegmentCacheV2) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.segments = make(map[string]string)
	c.hits = 0
	c.misses = 0
	c.totalSegments = 0
	c.totalSavedTokens = 0
}

// ---------------------------------------------------------------------------
// 内部辅助函数（以 psc 为前缀，避免命名冲突）
// ---------------------------------------------------------------------------

// pscEstimateTokens 粗略估算字符串的 token 数（约 4 字符/token）。
func pscEstimateTokens(s string) int {
	if len(s) == 0 {
		return 0
	}
	return (len(s) + 3) / 4
}

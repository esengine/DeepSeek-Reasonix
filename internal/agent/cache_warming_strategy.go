package agent

import "sync"

// ── OPT-157: CacheWarmingStrategy (缓存预热策略器 / Cache Warming Strategy) ──
// 根据访问模式制定缓存预热计划。记录每个 key 的访问次数，当访问次数超过
// 预热阈值且尚未被预热时，将其标记为预热候选者，支持提前加载热点数据到缓存。
//
// 原理：在 LLM 交互中，某些提示或请求会被频繁访问。通过追踪访问模式，
// 识别出高频访问的 key，在空闲时提前将其加载到缓存中，避免首次访问的延迟。
//
// 效果：减少缓存未命中导致的延迟，提高热点数据的响应速度，
// 统计预热次数和已预热 key 数量，为缓存策略优化提供依据。

// CacheWarmingStrategy 缓存预热策略器
type CacheWarmingStrategy struct {
	mu             sync.RWMutex
	accessPatterns map[string]int // key → 访问次数
	warmThreshold  int            // 预热阈值
	warmedKeys     map[string]bool // 已预热的 key 集合
	totalWarmed    int            // 累计预热次数
}

// NewCacheWarmingStrategy 创建缓存预热策略器。
// warmThreshold 指定预热阈值，访问次数超过该值的 key 将成为预热候选者。
// 若 warmThreshold <= 0 则默认为 3。
func NewCacheWarmingStrategy(warmThreshold int) *CacheWarmingStrategy {
	if warmThreshold <= 0 {
		warmThreshold = 3
	}
	return &CacheWarmingStrategy{
		accessPatterns: make(map[string]int),
		warmThreshold:  warmThreshold,
		warmedKeys:     make(map[string]bool),
	}
}

// RecordAccess 记录指定 key 的一次访问。
// key 为被访问的缓存键。
func (c *CacheWarmingStrategy) RecordAccess(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.accessPatterns[key]++
}

// ShouldWarm 判断指定 key 是否需要预热。
// 当 key 的访问次数超过预热阈值且尚未被预热过时返回 true。
func (c *CacheWarmingStrategy) ShouldWarm(key string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.warmedKeys[key] {
		return false
	}
	return c.accessPatterns[key] > c.warmThreshold
}

// MarkWarmed 标记指定 key 已完成预热。
// key 为已预热的缓存键，同时递增 totalWarmed 计数。
func (c *CacheWarmingStrategy) MarkWarmed(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.warmedKeys[key] {
		c.warmedKeys[key] = true
		c.totalWarmed++
	}
}

// GetWarmCandidates 获取所有需要预热的 key 列表。
// 返回访问次数超过阈值且尚未被预热的 key。
func (c *CacheWarmingStrategy) GetWarmCandidates() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return cwsGetCandidates(c.accessPatterns, c.warmedKeys, c.warmThreshold)
}

// GetStats 返回预热策略器的统计信息。
// 包含 trackedKeys（追踪的 key 数）、warmThreshold（预热阈值）、
// totalWarmed（累计预热次数）和 warmedCount（已预热 key 数）。
func (c *CacheWarmingStrategy) GetStats() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return map[string]interface{}{
		"trackedKeys":   len(c.accessPatterns),
		"warmThreshold": c.warmThreshold,
		"totalWarmed":   c.totalWarmed,
		"warmedCount":   len(c.warmedKeys),
	}
}

// Reset 重置预热策略器的所有状态和统计信息。
func (c *CacheWarmingStrategy) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.accessPatterns = make(map[string]int)
	c.warmedKeys = make(map[string]bool)
	c.totalWarmed = 0
}

// cwsGetCandidates 从访问模式中获取需要预热的候选 key 列表。
// 返回访问次数超过阈值且未被预热的 key。
func cwsGetCandidates(accessPatterns map[string]int, warmedKeys map[string]bool, threshold int) []string {
	var candidates []string
	for key, count := range accessPatterns {
		if count > threshold && !warmedKeys[key] {
			candidates = append(candidates, key)
		}
	}
	return candidates
}

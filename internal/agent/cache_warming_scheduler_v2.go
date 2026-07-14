package agent

import "sync"

// ── OPT-128: CacheWarmingSchedulerV2 (缓存预热调度器 V2) ──
// 基于缓存键的使用模式调度预热任务。通过学习缓存键的访问频率，
// 将高频键加入预热队列，批量处理以减少缓存未命中。
//
// 原理：频繁访问的缓存键更可能在后续被再次访问。
// CacheWarmingSchedulerV2 统计每个键的使用次数，允许将热门键
// 调度到预热队列，统一处理后提前载入缓存。
//
// 效果：减少冷启动缓存未命中，提升缓存命中率。

// CacheWarmingSchedulerV2 基于使用模式的缓存预热调度器。
type CacheWarmingSchedulerV2 struct {
	mu           sync.RWMutex
	patterns     map[string]int
	totalWarmed  int
	totalHits    int
	warmupQueue  []string
	maxQueueSize int
}

// NewCacheWarmingSchedulerV2 创建一个新的 CacheWarmingSchedulerV2。
// maxQueue 指定预热队列最大长度，若 <= 0 则默认 20。
func NewCacheWarmingSchedulerV2(maxQueue int) *CacheWarmingSchedulerV2 {
	if maxQueue <= 0 {
		maxQueue = 20
	}
	return &CacheWarmingSchedulerV2{
		patterns:     make(map[string]int),
		maxQueueSize: maxQueue,
	}
}

// LearnPattern 学习一个缓存键的使用模式，递增其访问计数。
func (s *CacheWarmingSchedulerV2) LearnPattern(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.patterns[key]++
}

// ScheduleWarmup 调度一个预热任务到队列。
// 若队列已满或键已在队列中，则不重复添加。
func (s *CacheWarmingSchedulerV2) ScheduleWarmup(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cwsContains(s.warmupQueue, key) {
		return
	}
	if len(s.warmupQueue) >= s.maxQueueSize {
		return
	}
	s.warmupQueue = append(s.warmupQueue, key)
}

// ProcessWarmupQueue 处理预热队列，返回需要预热的键列表。
// 处理后清空队列，并递增预热计数。
func (s *CacheWarmingSchedulerV2) ProcessWarmupQueue() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	result := make([]string, len(s.warmupQueue))
	copy(result, s.warmupQueue)
	s.totalWarmed += len(result)
	s.warmupQueue = s.warmupQueue[:0]
	return result
}

// RecordHit 记录一次缓存命中。
func (s *CacheWarmingSchedulerV2) RecordHit(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.totalHits++
}

// GetStats 返回调度器的统计信息。
func (s *CacheWarmingSchedulerV2) GetStats() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	hitRate := 0.0
	if s.totalWarmed > 0 {
		hitRate = float64(s.totalHits) / float64(s.totalWarmed)
	}

	return map[string]interface{}{
		"totalWarmed":  s.totalWarmed,
		"totalHits":    s.totalHits,
		"hitRate":      hitRate,
		"queueSize":    len(s.warmupQueue),
		"patternCount": len(s.patterns),
	}
}

// Reset 清除所有模式、队列和统计信息。
func (s *CacheWarmingSchedulerV2) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.patterns = make(map[string]int)
	s.warmupQueue = nil
	s.totalWarmed = 0
	s.totalHits = 0
}

// cwsContains 检查队列中是否已包含指定键。
func cwsContains(queue []string, key string) bool {
	for _, k := range queue {
		if k == key {
			return true
		}
	}
	return false
}

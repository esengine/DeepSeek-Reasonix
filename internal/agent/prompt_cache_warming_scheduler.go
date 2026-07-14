package agent
import "sync"

// ── OPT-210: PromptCacheWarmingScheduler (提示缓存预热调度器 / Prompt Cache Warming Scheduler) ──
// 调度缓存预热任务以最大化命中率。根据优先级调度提示缓存预热，
// 优先预热高优先级的 key，避免低优先级任务占用预热资源。
//
// 原理：提示缓存（prompt cache）的命中率直接影响 LLM 调用的延迟和成本。
// 通过提前预热高频使用的提示前缀，可以在实际请求到来时命中缓存，
// 减少重复计算。调度器按优先级排序预热任务，确保最重要的缓存先被预热。
//
// 效果：统计调度次数、已预热次数和跳过次数，
// 为缓存预热策略优化提供数据支撑。

// PromptCacheWarmingScheduler 提示缓存预热调度器
type PromptCacheWarmingScheduler struct {
	mu             sync.RWMutex
	schedule       map[string]int  // 待预热 key -> 优先级 pending key -> priority
	warmedKeys     map[string]bool // 已预热的 key warmed keys
	scheduledCount int             // 调度次数 scheduled count
	warmedCount    int             // 已预热次数 warmed count
	skippedCount   int             // 跳过次数（已预热或重复） skipped count
}

// NewPromptCacheWarmingScheduler 创建提示缓存预热调度器。
func NewPromptCacheWarmingScheduler() *PromptCacheWarmingScheduler {
	return &PromptCacheWarmingScheduler{
		schedule:   make(map[string]int),
		warmedKeys: make(map[string]bool),
	}
}

// Schedule 调度一个预热任务。
// 若 key 已预热则跳过（skippedCount++）；
// 若 key 已在调度队列中则更新优先级；否则新增到调度队列（scheduledCount++）。
// priority 值越大优先级越高。
func (s *PromptCacheWarmingScheduler) Schedule(key string, priority int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 已预热的 key 跳过
	if s.warmedKeys[key] {
		s.skippedCount++
		return
	}

	// 已在调度队列中则更新优先级
	if _, exists := s.schedule[key]; exists {
		s.schedule[key] = priority
		return
	}

	// 新增调度任务
	s.schedule[key] = priority
	s.scheduledCount++
}

// GetNext 获取下一个要预热的 key（优先级最高的）。
// 返回 (key, true) 表示获取成功，("", false) 表示调度队列为空。
// 此方法不会从调度队列中移除 key，需在预热完成后调用 MarkWarmed。
func (s *PromptCacheWarmingScheduler) GetNext() (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return pcwsFindHighestPriority(s.schedule)
}

// MarkWarmed 将 key 标记为已预热。
// 从调度队列中移除并加入已预热集合（warmedCount++）。
// 若 key 不在调度队列中但仍标记为已预热，则仅加入已预热集合。
func (s *PromptCacheWarmingScheduler) MarkWarmed(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.schedule, key)
	if !s.warmedKeys[key] {
		s.warmedKeys[key] = true
		s.warmedCount++
	}
}

// IsWarmed 检查 key 是否已预热。
func (s *PromptCacheWarmingScheduler) IsWarmed(key string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.warmedKeys[key]
}

// GetStats 返回调度器的统计信息。
// 包含 scheduledCount、warmedCount、skippedCount 和 pendingCount（待预热数量）。
func (s *PromptCacheWarmingScheduler) GetStats() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return map[string]interface{}{
		"scheduledCount": s.scheduledCount,
		"warmedCount":    s.warmedCount,
		"skippedCount":   s.skippedCount,
		"pendingCount":   len(s.schedule),
	}
}

// Reset 重置调度器的所有计数和集合。
func (s *PromptCacheWarmingScheduler) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.schedule = make(map[string]int)
	s.warmedKeys = make(map[string]bool)
	s.scheduledCount = 0
	s.warmedCount = 0
	s.skippedCount = 0
}

// pcwsFindHighestPriority 从调度 map 中查找优先级最高的 key。
// 优先级值越大越高。返回 (key, true) 表示找到，("", false) 表示 map 为空。
func pcwsFindHighestPriority(schedule map[string]int) (string, bool) {
	if len(schedule) == 0 {
		return "", false
	}

	var bestKey string
	var bestPriority int
	found := false

	for key, priority := range schedule {
		if !found || priority > bestPriority {
			bestKey = key
			bestPriority = priority
			found = true
		}
	}

	return bestKey, found
}

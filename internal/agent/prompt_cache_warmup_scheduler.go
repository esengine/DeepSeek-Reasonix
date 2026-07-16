package agent

import "sync"

// ── OPT-260: PromptCacheWarmupScheduler (提示缓存预热调度器) ──
// 将需要预热的提示缓存 key 排队，按序预热并记录已预热状态，避免重复预热。

// PromptCacheWarmupScheduler 提示缓存预热调度器。
type PromptCacheWarmupScheduler struct {
	mu             sync.RWMutex
	warmupQueue    []string        // 待预热 key 队列
	warmedUp       map[string]bool // 已预热 key 集合
	totalScheduled int             // 累计成功调度次数
	totalWarmed    int             // 累计预热完成次数
	maxQueueSize   int             // 队列最大容量
}

// NewPromptCacheWarmupScheduler 创建一个新的提示缓存预热调度器。
// maxQueueSize 指定队列最大容量，< 0 时视为 0。
func NewPromptCacheWarmupScheduler(maxQueueSize int) *PromptCacheWarmupScheduler {
	if maxQueueSize < 0 {
		maxQueueSize = 0
	}
	return &PromptCacheWarmupScheduler{
		warmupQueue:    make([]string, 0),
		warmedUp:       make(map[string]bool),
		totalScheduled: 0,
		totalWarmed:    0,
		maxQueueSize:   maxQueueSize,
	}
}

// Schedule 将 key 加入预热队列。
// 若 key 已预热或已在队列中，或队列已满，则返回 false。
func (s *PromptCacheWarmupScheduler) Schedule(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.warmedUp[key] {
		return false
	}
	for _, k := range s.warmupQueue {
		if k == key {
			return false
		}
	}
	if len(s.warmupQueue) >= s.maxQueueSize {
		return false
	}
	s.warmupQueue = append(s.warmupQueue, key)
	s.totalScheduled++
	return true
}

// WarmUpNext 预热队列中的下一个 key。
// 返回被预热的 key 与 true；队列为空时返回 "" 与 false。
func (s *PromptCacheWarmupScheduler) WarmUpNext() (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.warmupQueue) == 0 {
		return "", false
	}
	key := s.warmupQueue[0]
	s.warmupQueue = pcwsShiftQueue(s.warmupQueue)
	s.warmedUp[key] = true
	s.totalWarmed++
	return key, true
}

// IsWarmedUp 返回 key 是否已预热。
func (s *PromptCacheWarmupScheduler) IsWarmedUp(key string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.warmedUp[key]
}

// GetQueueSize 返回当前待预热的 key 数量。
func (s *PromptCacheWarmupScheduler) GetQueueSize() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return len(s.warmupQueue)
}

// GetStats 返回预热调度器的统计信息。
// 包含: maxQueueSize, queueSize, totalScheduled, totalWarmed, warmedCount。
func (s *PromptCacheWarmupScheduler) GetStats() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return map[string]interface{}{
		"maxQueueSize":   s.maxQueueSize,
		"queueSize":      len(s.warmupQueue),
		"totalScheduled": s.totalScheduled,
		"totalWarmed":    s.totalWarmed,
		"warmedCount":    len(s.warmedUp),
	}
}

// Reset 重置预热调度器，清空队列与已预热集合及计数，保留 maxQueueSize 配置。
func (s *PromptCacheWarmupScheduler) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.warmupQueue = make([]string, 0)
	s.warmedUp = make(map[string]bool)
	s.totalScheduled = 0
	s.totalWarmed = 0
}

// ---------------------------------------------------------------------------
// 辅助函数（以 pcws 为前缀，避免命名冲突）
// ---------------------------------------------------------------------------

// pcwsShiftQueue 移除队列首元素并返回剩余部分。
func pcwsShiftQueue(queue []string) []string {
	if len(queue) == 0 {
		return queue
	}
	return queue[1:]
}

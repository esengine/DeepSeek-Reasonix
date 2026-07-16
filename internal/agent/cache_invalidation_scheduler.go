package agent

import (
	"sort"
	"sync"
)

// ── OPT-257: CacheInvalidationScheduler (缓存失效调度器) ──
// 调度延迟缓存失效任务：按 key 登记计划失效时间，到期后由 Execute 批量执行。
// 显式维护 pendingCount，并累计已执行与已取消的失效任务数量，便于统计调度器运行状况。

// CacheInvalidationScheduler 缓存失效调度器，按计划时间延迟触发缓存失效。
type CacheInvalidationScheduler struct {
	mu             sync.RWMutex
	schedule       map[string]int64 // key → 计划失效时间
	executedCount  int              // 已执行的失效任务数
	cancelledCount int              // 已取消的失效任务数
	pendingCount   int              // 当前待执行的失效任务数
}

// NewCacheInvalidationScheduler 创建一个新的缓存失效调度器。
func NewCacheInvalidationScheduler() *CacheInvalidationScheduler {
	return &CacheInvalidationScheduler{
		schedule:       make(map[string]int64),
		executedCount:  0,
		cancelledCount: 0,
		pendingCount:   0,
	}
}

// Schedule 调度一次延迟失效任务，登记 key 的计划失效时间 executeAt。
// 若 key 已存在则更新其计划时间（pendingCount 不重复计数）。
func (s *CacheInvalidationScheduler) Schedule(key string, executeAt int64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.schedule[key]; !exists {
		s.pendingCount++
	}
	s.schedule[key] = executeAt
}

// Execute 执行所有到期（scheduledTime <= currentTime）的失效任务，
// 返回本次失效的 key 列表（按计划时间升序），并递增 executedCount。
func (s *CacheInvalidationScheduler) Execute(currentTime int64) []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	expired := cisFindExpired(s.schedule, currentTime)
	if len(expired) == 0 {
		return []string{}
	}

	// 按计划时间升序排序，保证执行顺序确定。
	sort.Slice(expired, func(i, j int) bool {
		return s.schedule[expired[i]] < s.schedule[expired[j]]
	})

	for _, k := range expired {
		delete(s.schedule, k)
	}
	s.executedCount += len(expired)
	s.pendingCount -= len(expired)
	return expired
}

// Cancel 取消指定 key 的失效任务。
// 若 key 存在计划任务则删除并递增 cancelledCount，返回 true；否则返回 false。
func (s *CacheInvalidationScheduler) Cancel(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.schedule[key]; !ok {
		return false
	}
	delete(s.schedule, key)
	s.cancelledCount++
	s.pendingCount--
	return true
}

// GetPendingCount 返回当前待执行的失效任务数量。
func (s *CacheInvalidationScheduler) GetPendingCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.pendingCount
}

// GetStats 返回调度器的统计信息。
// 包含: executedCount, cancelledCount, pendingCount。
func (s *CacheInvalidationScheduler) GetStats() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return map[string]interface{}{
		"executedCount":  s.executedCount,
		"cancelledCount": s.cancelledCount,
		"pendingCount":   s.pendingCount,
	}
}

// Reset 重置调度器，清空待执行任务与计数。
func (s *CacheInvalidationScheduler) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.schedule = make(map[string]int64)
	s.executedCount = 0
	s.cancelledCount = 0
	s.pendingCount = 0
}

// ---------------------------------------------------------------------------
// 辅助函数（以 cis 为前缀，避免命名冲突）
// ---------------------------------------------------------------------------

// cisFindExpired 收集所有计划失效时间 <= currentTime 的 key。
func cisFindExpired(schedule map[string]int64, currentTime int64) []string {
	var expired []string
	for k, at := range schedule {
		if at <= currentTime {
			expired = append(expired, k)
		}
	}
	return expired
}

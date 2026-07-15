package agent
import "sync"

// ── OPT-217: CacheInvalidationScheduler (缓存失效调度器) ──
// 调度延迟缓存失效任务：按 key 登记计划失效时间，到期后由 Execute 批量执行。
// 累计已执行与已取消的失效任务数量，便于统计调度器运行状况。

// CacheInvalidationScheduler 缓存失效调度器，按计划时间延迟触发缓存失效。
type CacheInvalidationScheduler struct {
	mu             sync.RWMutex
	pending        map[string]int64 // key → 计划失效时间
	executedCount  int              // 已执行的失效任务数
	cancelledCount int              // 已取消的失效任务数
	maxDelay       int              // 允许的最大延迟（单位由调用方约定）
}

// NewCacheInvalidationScheduler 创建一个新的缓存失效调度器。
// maxDelay 指定允许的最大延迟，若 <= 0 则默认为 0（不限制）。
func NewCacheInvalidationScheduler(maxDelay int) *CacheInvalidationScheduler {
	return &CacheInvalidationScheduler{
		pending:  make(map[string]int64),
		maxDelay: maxDelay,
	}
}

// Schedule 调度一次延迟失效任务，登记 key 的计划失效时间 atTime。
func (s *CacheInvalidationScheduler) Schedule(key string, atTime int64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.pending[key] = atTime
}

// Execute 执行所有到期（scheduledTime <= currentTime）的失效任务，
// 返回本次失效的 key 列表，并递增 executedCount。
func (s *CacheInvalidationScheduler) Execute(currentTime int64) []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	expired := cisCollectExpired(s.pending, currentTime)
	for _, k := range expired {
		delete(s.pending, k)
	}
	s.executedCount += len(expired)
	return expired
}

// Cancel 取消指定 key 的失效任务。
// 若 key 存在计划任务则删除并递增 cancelledCount，返回 true；否则返回 false。
func (s *CacheInvalidationScheduler) Cancel(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.pending[key]; !ok {
		return false
	}
	delete(s.pending, key)
	s.cancelledCount++
	return true
}

// GetPendingCount 返回当前待执行的失效任务数量。
func (s *CacheInvalidationScheduler) GetPendingCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return len(s.pending)
}

// GetStats 返回调度器的统计信息。
// 包含: pendingCount, executedCount, cancelledCount, maxDelay。
func (s *CacheInvalidationScheduler) GetStats() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return map[string]interface{}{
		"pendingCount":   len(s.pending),
		"executedCount":  s.executedCount,
		"cancelledCount": s.cancelledCount,
		"maxDelay":       s.maxDelay,
	}
}

// Reset 重置调度器，清空待执行任务与计数，保留 maxDelay 配置。
func (s *CacheInvalidationScheduler) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.pending = make(map[string]int64)
	s.executedCount = 0
	s.cancelledCount = 0
}

// ---------------------------------------------------------------------------
// 辅助函数（以 cis 为前缀，避免命名冲突）
// ---------------------------------------------------------------------------

// cisCollectExpired 收集所有计划失效时间 <= currentTime 的 key。
func cisCollectExpired(pending map[string]int64, currentTime int64) []string {
	var expired []string
	for k, at := range pending {
		if at <= currentTime {
			expired = append(expired, k)
		}
	}
	return expired
}

package agent
import "sync"

// ── OPT-162: CachePrefetchScheduler (缓存预取调度器 / Cache Prefetch Scheduler) ──
// 基于预测提前将缓存内容加入预取队列，并按优先级调度执行。
// 高优先级（数值越大越优先）的任务会被最先取出执行，从而在真正请求
// 到达前完成预热，减少缓存未命中。
//
// 原理：若能预测后续会访问的缓存键，可提前异步预取，使后续请求直接命中缓存。
//
// 效果：减少冷启动与首字延迟，提升缓存命中率。

// PrefetchTask 预取任务，包含缓存键、优先级与预估 token 数。
type PrefetchTask struct {
	Key             string
	Priority        int
	EstimatedTokens int
}

// CachePrefetchScheduler 缓存预取调度器，按优先级降序调度预取任务。
type CachePrefetchScheduler struct {
	mu             sync.RWMutex
	prefetchQueue  []PrefetchTask
	activeTasks    int
	maxConcurrent  int
	completedCount int
	failedCount    int
}

// NewCachePrefetchScheduler 创建一个新的 CachePrefetchScheduler。
// maxConcurrent 指定最大并发预取数，若 <=0 则默认 4。
func NewCachePrefetchScheduler(maxConcurrent int) *CachePrefetchScheduler {
	if maxConcurrent <= 0 {
		maxConcurrent = 4
	}
	return &CachePrefetchScheduler{
		maxConcurrent: maxConcurrent,
	}
}

// Schedule 将一个预取任务加入队列，并重新按优先级排序。
func (s *CachePrefetchScheduler) Schedule(key string, priority int, estimatedTokens int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prefetchQueue = append(s.prefetchQueue, PrefetchTask{
		Key:             key,
		Priority:        priority,
		EstimatedTokens: estimatedTokens,
	})
	cpsSortQueue(s.prefetchQueue)
}

// Next 取出优先级最高的任务（队列首位）。
// 取出后活动任务计数加一。队列为空时返回零值与 false。
func (s *CachePrefetchScheduler) Next() (PrefetchTask, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.prefetchQueue) == 0 {
		return PrefetchTask{}, false
	}
	task := s.prefetchQueue[0]
	s.prefetchQueue = s.prefetchQueue[1:]
	s.activeTasks++
	return task, true
}

// Complete 标记一个任务完成，递减活动任务计数，并按成功与否更新完成/失败计数。
// key 用于标识被完成的任务（仅用于语义完整性，计数按总数维护）。
func (s *CachePrefetchScheduler) Complete(key string, success bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = key
	if s.activeTasks > 0 {
		s.activeTasks--
	}
	if success {
		s.completedCount++
	} else {
		s.failedCount++
	}
}

// GetQueueSize 返回当前队列中待执行的任务数。
func (s *CachePrefetchScheduler) GetQueueSize() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.prefetchQueue)
}

// GetStats 返回调度器的统计信息，包括 queueSize、maxConcurrent、completedCount 和 failedCount。
func (s *CachePrefetchScheduler) GetStats() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return map[string]interface{}{
		"queueSize":      len(s.prefetchQueue),
		"maxConcurrent":  s.maxConcurrent,
		"completedCount": s.completedCount,
		"failedCount":    s.failedCount,
	}
}

// Reset 重置调度器的所有状态，清空队列与计数（保留 maxConcurrent 配置）。
func (s *CachePrefetchScheduler) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prefetchQueue = nil
	s.activeTasks = 0
	s.completedCount = 0
	s.failedCount = 0
}

// cpsSortQueue 按优先级降序稳定排序预取队列（插入排序）。
func cpsSortQueue(queue []PrefetchTask) {
	for i := 1; i < len(queue); i++ {
		key := queue[i]
		j := i - 1
		for j >= 0 && queue[j].Priority < key.Priority {
			queue[j+1] = queue[j]
			j--
		}
		queue[j+1] = key
	}
}

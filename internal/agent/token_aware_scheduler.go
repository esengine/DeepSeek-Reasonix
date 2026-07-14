package agent

import "sync"

// ── OPT-107: TokenAwareScheduler (Token 感知调度器) ──
// 根据 token 预算调度任务优先级，确保高优先级且 token 开销
// 在预算内的任务优先执行。
//
// 原理：每个任务携带优先级与 token 预估。Next() 在所有
// tokenEstimate <= budgetPerTask 的任务中选取优先级最高的执行，
// 避免低优先级大 token 任务挤占预算。
//
// 效果：在 token 受限环境下优先保障关键任务，提升整体 token 效率。

// SchedulerTask 调度器任务。
type SchedulerTask struct {
	ID            int
	Priority      int
	TokenEstimate int
	Description   string
}

// TokenAwareScheduler Token 感知调度器。
type TokenAwareScheduler struct {
	mu             sync.RWMutex
	tasks          map[int]SchedulerTask
	nextID         int
	totalScheduled int
	totalExecuted  int
	budgetPerTask  int
}

// NewTokenAwareScheduler 创建新的 Token 感知调度器，
// budgetPerTask 为单个任务的 token 预算上限。
func NewTokenAwareScheduler(budgetPerTask int) *TokenAwareScheduler {
	return &TokenAwareScheduler{
		tasks:         make(map[int]SchedulerTask),
		budgetPerTask: budgetPerTask,
	}
}

// Schedule 创建一个新任务并返回其 ID。
// description 为任务描述，priority 为优先级（数值越大优先级越高），
// tokenEstimate 为预估 token 开销。
func (s *TokenAwareScheduler) Schedule(description string, priority int, tokenEstimate int) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.nextID++
	id := s.nextID
	s.tasks[id] = SchedulerTask{
		ID:            id,
		Priority:      priority,
		TokenEstimate: tokenEstimate,
		Description:   description,
	}
	s.totalScheduled++
	return id
}

// Next 返回优先级最高且 tokenEstimate <= budgetPerTask 的任务。
// 若没有符合条件的任务则返回 nil。
func (s *TokenAwareScheduler) Next() *SchedulerTask {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var best *SchedulerTask
	for _, task := range s.tasks {
		if task.TokenEstimate > s.budgetPerTask {
			continue
		}
		t := task
		if best == nil {
			best = &t
			continue
		}
		if tasHigherPriority(&t, best) {
			best = &t
		}
	}
	return best
}

// Complete 标记指定任务完成并从调度队列中删除。
func (s *TokenAwareScheduler) Complete(taskID int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.tasks[taskID]; ok {
		delete(s.tasks, taskID)
		s.totalExecuted++
	}
}

// GetStats 返回调度器统计信息，包括 totalScheduled、totalExecuted、
// pendingTasks 和 budgetPerTask。
func (s *TokenAwareScheduler) GetStats() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return map[string]interface{}{
		"totalScheduled": s.totalScheduled,
		"totalExecuted":  s.totalExecuted,
		"pendingTasks":   len(s.tasks),
		"budgetPerTask":  s.budgetPerTask,
	}
}

// Reset 重置调度器状态，清空所有任务和计数。
func (s *TokenAwareScheduler) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tasks = make(map[int]SchedulerTask)
	s.nextID = 0
	s.totalScheduled = 0
	s.totalExecuted = 0
}

// tasHigherPriority 判断任务 a 是否比 b 优先级更高。
// 优先级数值越大越优先；数值相同时 ID 较小者优先。
func tasHigherPriority(a, b *SchedulerTask) bool {
	if a.Priority != b.Priority {
		return a.Priority > b.Priority
	}
	return a.ID < b.ID
}

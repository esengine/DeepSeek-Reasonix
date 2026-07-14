package agent
import "sync"

// ── OPT-181: TokenAwareSchedulerV2 (Token 感知调度器 V2 / Token-Aware Scheduler V2) ──
// 结合任务优先级与 token 预算进行智能调度。优先级高的任务先调度，
// 同优先级下 token 预算小的任务先调度，以在 token 受限环境中最大化
// 关键任务的执行概率并降低 token 浪费。
//
// 与 OPT-107 TokenAwareScheduler 的区别：V2 以任务切片维护待调度队列，
// 调度策略同时考虑优先级与 token 预算，并提供 ScheduleNext 弹出语义。

// SchedV2Task 调度器 V2 的任务单元。
type SchedV2Task struct {
	ID          string // 任务唯一标识
	Priority    int    // 优先级（数值越大越优先）
	TokenBudget int    // 该任务预估的 token 预算
	Status      string // 任务状态
}

// TokenAwareSchedulerV2 Token 感知调度器 V2。
type TokenAwareSchedulerV2 struct {
	mu                   sync.RWMutex
	tasks                []SchedV2Task
	scheduledCount       int
	totalTokensScheduled int
}

// NewTokenAwareSchedulerV2 创建新的 Token 感知调度器 V2。
func NewTokenAwareSchedulerV2() *TokenAwareSchedulerV2 {
	return &TokenAwareSchedulerV2{
		tasks: make([]SchedV2Task, 0),
	}
}

// AddTask 添加一个待调度任务。
// id 为任务唯一标识，priority 为优先级（越大越优先），
// tokenBudget 为该任务的 token 预算。
func (s *TokenAwareSchedulerV2) AddTask(id string, priority int, tokenBudget int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.tasks = append(s.tasks, SchedV2Task{
		ID:          id,
		Priority:    priority,
		TokenBudget: tokenBudget,
		Status:      "pending",
	})
}

// ScheduleNext 按优先级与 token 预算调度下一个任务。
// 优先级高的先调度；同优先级下 token 预算小的先调度。
// 弹出并返回该任务，同时累加调度计数与已调度 token 总量。
// 若队列为空则返回零值与 false。
func (s *TokenAwareSchedulerV2) ScheduleNext() (SchedV2Task, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.tasks) == 0 {
		return SchedV2Task{}, false
	}

	// 排序：优先级降序，同优先级 token 预算升序
	tas2SortTasks(s.tasks)

	task := s.tasks[0]
	task.Status = "scheduled"
	s.tasks = s.tasks[1:]

	s.scheduledCount++
	s.totalTokensScheduled += task.TokenBudget
	return task, true
}

// GetPendingCount 返回当前待调度任务数量。
func (s *TokenAwareSchedulerV2) GetPendingCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.tasks)
}

// GetStats 返回调度器统计信息，包括 pendingCount、scheduledCount
// 与 totalTokensScheduled。
func (s *TokenAwareSchedulerV2) GetStats() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return map[string]interface{}{
		"pendingCount":         len(s.tasks),
		"scheduledCount":       s.scheduledCount,
		"totalTokensScheduled": s.totalTokensScheduled,
	}
}

// Reset 重置调度器状态，清空任务队列与所有计数。
func (s *TokenAwareSchedulerV2) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.tasks = make([]SchedV2Task, 0)
	s.scheduledCount = 0
	s.totalTokensScheduled = 0
}

// tas2SortTasks 对任务切片就地排序：优先级降序，同优先级按 token 预算升序。
func tas2SortTasks(tasks []SchedV2Task) {
	for i := 1; i < len(tasks); i++ {
		for j := i; j > 0; j-- {
			a := tasks[j]
			b := tasks[j-1]
			// 优先级高的在前
			if a.Priority > b.Priority {
				tasks[j], tasks[j-1] = tasks[j-1], tasks[j]
				continue
			}
			// 同优先级 token 预算小的在前
			if a.Priority == b.Priority && a.TokenBudget < b.TokenBudget {
				tasks[j], tasks[j-1] = tasks[j-1], tasks[j]
				continue
			}
			break
		}
	}
}

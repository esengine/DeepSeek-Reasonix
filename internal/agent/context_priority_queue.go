package agent

import "sync"

// ── OPT-142: ContextPriorityQueue (上下文优先级队列) ──
// 按优先级管理上下文消息。入队时将项目追加到队列尾部，
// 出队时选取优先级最高的项返回（优先级数值越大优先级越高）。
// 队列容量受 maxSize 限制，超出时拒绝入队。

// PriorityItem 优先级队列中的一个项。
type PriorityItem struct {
	Content  string
	Priority int
	Tokens   int
}

// ContextPriorityQueue 上下文优先级队列，按优先级管理上下文消息。
type ContextPriorityQueue struct {
	mu            sync.RWMutex
	items         []PriorityItem
	totalEnqueued int
	totalDequeued int
	maxSize       int
}

// NewContextPriorityQueue 创建一个新的上下文优先级队列。
// maxSize 为队列允许的最大长度。
func NewContextPriorityQueue(maxSize int) *ContextPriorityQueue {
	return &ContextPriorityQueue{
		items:   make([]PriorityItem, 0),
		maxSize: maxSize,
	}
}

// Enqueue 将项目入队。
// 若队列已满（达到 maxSize），则拒绝入队并返回 false。
func (q *ContextPriorityQueue) Enqueue(item PriorityItem) bool {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.items) >= q.maxSize {
		return false
	}
	q.items = append(q.items, item)
	q.totalEnqueued++
	return true
}

// Dequeue 出队最高优先级项。
// 优先级数值越大优先级越高；队列为空时返回零值和 false。
func (q *ContextPriorityQueue) Dequeue() (PriorityItem, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.items) == 0 {
		return PriorityItem{}, false
	}

	idx := cpqFindMaxPriority(q.items)
	item := q.items[idx]
	q.items = append(q.items[:idx], q.items[idx+1:]...)
	q.totalDequeued++
	return item, true
}

// Peek 查看队首（最高优先级项）但不移除。
// 队列为空时返回零值和 false。
func (q *ContextPriorityQueue) Peek() (PriorityItem, bool) {
	q.mu.RLock()
	defer q.mu.RUnlock()

	if len(q.items) == 0 {
		return PriorityItem{}, false
	}

	idx := cpqFindMaxPriority(q.items)
	return q.items[idx], true
}

// GetStats 返回队列的统计信息。
// 包含: totalEnqueued, totalDequeued, currentSize, maxSize。
func (q *ContextPriorityQueue) GetStats() map[string]interface{} {
	q.mu.RLock()
	defer q.mu.RUnlock()

	return map[string]interface{}{
		"totalEnqueued": q.totalEnqueued,
		"totalDequeued": q.totalDequeued,
		"currentSize":   len(q.items),
		"maxSize":       q.maxSize,
	}
}

// Reset 重置队列，清空所有项目并归零统计计数。
func (q *ContextPriorityQueue) Reset() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.items = make([]PriorityItem, 0)
	q.totalEnqueued = 0
	q.totalDequeued = 0
}

// cpqFindMaxPriority 查找优先级最高的项的索引。
// 优先级数值越大优先级越高；相同优先级时保留先入队项。
func cpqFindMaxPriority(items []PriorityItem) int {
	maxIdx := 0
	for i := 1; i < len(items); i++ {
		if items[i].Priority > items[maxIdx].Priority {
			maxIdx = i
		}
	}
	return maxIdx
}

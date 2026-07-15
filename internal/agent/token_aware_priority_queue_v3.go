package agent

import "sync"

// ── OPT-211: TokenAwarePriorityQueueV3 (Token感知优先级队列V3) ──
// 支持多级优先级和token预算的优先级队列。
// 出队时优先级最高的元素先出，同优先级时保持入队顺序（FIFO）。
// 入队时若队列已满（达到maxItems）则拒绝入队并返回false。
//
// 与V2不同，V3使用无序切片存储，出队时线性查找最高优先级元素，
// 适用于元素数量较少但优先级动态变化的场景。

// PQ3Item 表示优先级队列V3中的一个元素。
type PQ3Item struct {
	// ID 是元素的唯一标识符
	ID string
	// Priority 表示优先级，数值越大优先级越高
	Priority int
	// TokenBudget 表示该元素关联的token预算
	TokenBudget int
}

// TokenAwarePriorityQueueV3 是Token感知优先级队列V3。
type TokenAwarePriorityQueueV3 struct {
	mu            sync.RWMutex
	items         []PQ3Item
	totalEnqueued int
	totalDequeued int
	maxItems      int
}

// NewTokenAwarePriorityQueueV3 创建一个新的TokenAwarePriorityQueueV3实例。
// maxItems 指定队列的最大容量，超过此数量时Enqueue将返回false。
func NewTokenAwarePriorityQueueV3(maxItems int) *TokenAwarePriorityQueueV3 {
	return &TokenAwarePriorityQueueV3{
		items:    make([]PQ3Item, 0),
		maxItems: maxItems,
	}
}

// Enqueue 将一个元素入队。若队列已满（达到maxItems）则返回false。
func (q *TokenAwarePriorityQueueV3) Enqueue(id string, priority int, tokenBudget int) bool {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.items) >= q.maxItems {
		return false
	}

	item := PQ3Item{
		ID:          id,
		Priority:    priority,
		TokenBudget: tokenBudget,
	}
	q.items = append(q.items, item)
	q.totalEnqueued++
	return true
}

// Dequeue 从队列中取出优先级最高的元素。
// 若队列为空返回零值和false。
func (q *TokenAwarePriorityQueueV3) Dequeue() (PQ3Item, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	idx := pq3FindHighest(q.items)
	if idx < 0 {
		return PQ3Item{}, false
	}
	item := q.items[idx]
	q.items = append(q.items[:idx], q.items[idx+1:]...)
	q.totalDequeued++
	return item, true
}

// Peek 查看队首（优先级最高）的元素但不移除。
// 若队列为空返回零值和false。
func (q *TokenAwarePriorityQueueV3) Peek() (PQ3Item, bool) {
	q.mu.RLock()
	defer q.mu.RUnlock()

	idx := pq3FindHighest(q.items)
	if idx < 0 {
		return PQ3Item{}, false
	}
	return q.items[idx], true
}

// Count 返回当前队列中的元素数量。
func (q *TokenAwarePriorityQueueV3) Count() int {
	q.mu.RLock()
	defer q.mu.RUnlock()

	return len(q.items)
}

// GetStats 返回队列的统计信息。
func (q *TokenAwarePriorityQueueV3) GetStats() map[string]interface{} {
	q.mu.RLock()
	defer q.mu.RUnlock()

	return map[string]interface{}{
		"maxItems":      q.maxItems,
		"currentCount":  len(q.items),
		"totalEnqueued": q.totalEnqueued,
		"totalDequeued": q.totalDequeued,
	}
}

// Reset 重置队列为初始状态。
func (q *TokenAwarePriorityQueueV3) Reset() {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.items = make([]PQ3Item, 0)
	q.totalEnqueued = 0
	q.totalDequeued = 0
}

// pq3FindHighest 在items中查找优先级最高的元素的索引。
// 优先级数值越大优先级越高，同优先级时返回先入队的元素（索引较小者）。
// 若切片为空返回-1。
func pq3FindHighest(items []PQ3Item) int {
	if len(items) == 0 {
		return -1
	}
	bestIdx := 0
	bestPriority := items[0].Priority
	for i := 1; i < len(items); i++ {
		if items[i].Priority > bestPriority {
			bestPriority = items[i].Priority
			bestIdx = i
		}
	}
	return bestIdx
}

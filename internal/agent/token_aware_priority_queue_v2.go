package agent

import "sync"

// OPT-196: TokenAwarePriorityQueueV2 / Token感知优先队列V2
// 支持动态优先级调整的Token感知优先队列，出队时优先级最高者优先，
// 同优先级下Token开销小者优先。

// PQV2Item 表示优先队列中的一个元素。
type PQV2Item struct {
	// ID 是元素的唯一标识符
	ID string
	// Priority 表示优先级，数值越大优先级越高
	Priority int
	// TokenCost 表示该元素的Token开销
	TokenCost int
	// Data 表示元素携带的数据
	Data string
}

// TokenAwarePriorityQueueV2 是Token感知优先队列V2。
type TokenAwarePriorityQueueV2 struct {
	mu           sync.RWMutex
	items        []PQV2Item
	enqueueCount int
	dequeueCount int
}

// NewTokenAwarePriorityQueueV2 创建一个新的TokenAwarePriorityQueueV2实例。
func NewTokenAwarePriorityQueueV2() *TokenAwarePriorityQueueV2 {
	return &TokenAwarePriorityQueueV2{
		items:        make([]PQV2Item, 0),
		enqueueCount: 0,
		dequeueCount: 0,
	}
}

// Enqueue 将一个元素入队，按优先级和Token开销维护有序性。
func (q *TokenAwarePriorityQueueV2) Enqueue(id string, priority int, tokenCost int, data string) {
	q.mu.Lock()
	defer q.mu.Unlock()

	item := PQV2Item{
		ID:        id,
		Priority:  priority,
		TokenCost: tokenCost,
		Data:      data,
	}
	pos := tpq2FindInsertPos(q.items, item)
	// 在pos位置插入item
	q.items = append(q.items, PQV2Item{})
	copy(q.items[pos+1:], q.items[pos:])
	q.items[pos] = item
	q.enqueueCount++
}

// Dequeue 从队列中取出优先级最高（同优先级Token开销最小）的元素。
func (q *TokenAwarePriorityQueueV2) Dequeue() (PQV2Item, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.items) == 0 {
		return PQV2Item{}, false
	}
	item := q.items[0]
	q.items = q.items[1:]
	q.dequeueCount++
	return item, true
}

// UpdatePriority 更新指定ID元素的优先级，并重新维护队列有序性。
func (q *TokenAwarePriorityQueueV2) UpdatePriority(id string, newPriority int) bool {
	q.mu.Lock()
	defer q.mu.Unlock()

	idx := tpq2FindItem(q.items, id)
	if idx < 0 {
		return false
	}
	// 移除该元素
	item := q.items[idx]
	item.Priority = newPriority
	q.items = append(q.items[:idx], q.items[idx+1:]...)
	// 重新插入到正确位置
	pos := tpq2FindInsertPos(q.items, item)
	q.items = append(q.items, PQV2Item{})
	copy(q.items[pos+1:], q.items[pos:])
	q.items[pos] = item
	return true
}

// Peek 查看队首元素但不移除。
func (q *TokenAwarePriorityQueueV2) Peek() (PQV2Item, bool) {
	q.mu.RLock()
	defer q.mu.RUnlock()

	if len(q.items) == 0 {
		return PQV2Item{}, false
	}
	return q.items[0], true
}

// GetStats 返回队列的统计信息。
func (q *TokenAwarePriorityQueueV2) GetStats() map[string]interface{} {
	q.mu.RLock()
	defer q.mu.RUnlock()

	return map[string]interface{}{
		"queueSize":    len(q.items),
		"enqueueCount": q.enqueueCount,
		"dequeueCount": q.dequeueCount,
	}
}

// Reset 重置队列为初始状态。
func (q *TokenAwarePriorityQueueV2) Reset() {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.items = make([]PQV2Item, 0)
	q.enqueueCount = 0
	q.dequeueCount = 0
}

// tpq2FindInsertPos 在有序切片中找到新元素的插入位置。
// 排序规则：优先级降序，同优先级Token开销升序。
func tpq2FindInsertPos(items []PQV2Item, item PQV2Item) int {
	for i, existing := range items {
		if item.Priority > existing.Priority {
			return i
		}
		if item.Priority == existing.Priority && item.TokenCost < existing.TokenCost {
			return i
		}
	}
	return len(items)
}

// tpq2FindItem 根据ID查找元素在切片中的索引，未找到返回-1。
func tpq2FindItem(items []PQV2Item, id string) int {
	for i, existing := range items {
		if existing.ID == id {
			return i
		}
	}
	return -1
}

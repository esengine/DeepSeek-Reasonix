package agent
import "sync"

// ── OPT-242: CacheInvalidationPriorityQueue (缓存失效优先级队列 / Cache Invalidation Priority Queue) ──
// 按优先级处理缓存失效请求，确保高优先级失效优先执行。
// 入队时记录每个失效键的优先级，出队时取出优先级最高的条目。
// 优先级数值越大表示越紧迫。

// CIPQItem 缓存失效优先级队列条目
type CIPQItem struct {
	Key      string // 失效键
	Priority int    // 优先级（数值越大越先处理）
}

// CacheInvalidationPriorityQueue 缓存失效优先级队列
type CacheInvalidationPriorityQueue struct {
	mu            sync.RWMutex
	items         []CIPQItem // 待处理失效条目
	totalEnqueued int        // 累计入队次数
	totalDequeued int        // 累计出队次数
}

// NewCacheInvalidationPriorityQueue 创建一个新的缓存失效优先级队列实例。
func NewCacheInvalidationPriorityQueue() *CacheInvalidationPriorityQueue {
	return &CacheInvalidationPriorityQueue{
		items: make([]CIPQItem, 0),
	}
}

// Enqueue 将一个失效请求按指定优先级入队。
// key 为缓存键，priority 为优先级（数值越大越先处理）。
func (c *CacheInvalidationPriorityQueue) Enqueue(key string, priority int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = append(c.items, CIPQItem{Key: key, Priority: priority})
	c.totalEnqueued++
}

// Dequeue 出队优先级最高的条目。
// 返回该条目与是否成功；队列为空时返回零值与 false。
func (c *CacheInvalidationPriorityQueue) Dequeue() (CIPQItem, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	idx, ok := cipqFindHighest(c.items)
	if !ok {
		return CIPQItem{}, false
	}
	item := c.items[idx]
	c.items = append(c.items[:idx], c.items[idx+1:]...)
	c.totalDequeued++
	return item, true
}

// Count 返回当前队列中的条目数量。
func (c *CacheInvalidationPriorityQueue) Count() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.items)
}

// Peek 查看队首（优先级最高）条目但不移除。
// 返回该条目与是否成功；队列为空时返回零值与 false。
func (c *CacheInvalidationPriorityQueue) Peek() (CIPQItem, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	idx, ok := cipqFindHighest(c.items)
	if !ok {
		return CIPQItem{}, false
	}
	return c.items[idx], true
}

// GetStats 获取统计信息。
// 返回 currentCount、totalEnqueued、totalDequeued。
func (c *CacheInvalidationPriorityQueue) GetStats() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return map[string]interface{}{
		"currentCount":  len(c.items),
		"totalEnqueued": c.totalEnqueued,
		"totalDequeued": c.totalDequeued,
	}
}

// Reset 重置队列与累计统计信息。
func (c *CacheInvalidationPriorityQueue) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = make([]CIPQItem, 0)
	c.totalEnqueued = 0
	c.totalDequeued = 0
}

// cipqFindHighest 辅助函数，查找优先级最高的条目索引。
// 多个条目优先级相同时返回最先出现的那个。
// 返回索引与是否找到；队列为空时返回 (0, false)。
func cipqFindHighest(items []CIPQItem) (int, bool) {
	if len(items) == 0 {
		return 0, false
	}
	maxIdx := 0
	for i := 1; i < len(items); i++ {
		if items[i].Priority > items[maxIdx].Priority {
			maxIdx = i
		}
	}
	return maxIdx, true
}

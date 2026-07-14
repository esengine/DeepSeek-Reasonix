package agent

import "sync"

// ── OPT-170: PromptCacheWarmer (提示缓存预热器) ──
// 预热常用提示以提升首次响应速度。
// 通过优先级队列管理预热任务，支持标记预热结果和查重。

// WarmupTask 预热任务
type WarmupTask struct {
	PromptHash    string
	PromptContent string
	Priority      int
}

// PromptCacheWarmer 提示缓存预热器
type PromptCacheWarmer struct {
	mu            sync.RWMutex
	warmupQueue   []WarmupTask
	warmedPrompts map[string]bool
	totalWarmed   int
	totalFailed   int
	maxQueueSize  int
}

// NewPromptCacheWarmer 创建提示缓存预热器
func NewPromptCacheWarmer(maxQueueSize int) *PromptCacheWarmer {
	return &PromptCacheWarmer{
		warmupQueue:   make([]WarmupTask, 0),
		warmedPrompts: make(map[string]bool),
		maxQueueSize:  maxQueueSize,
	}
}

// Enqueue 加入预热队列（超过 maxQueueSize 返回 false）
func (w *PromptCacheWarmer) Enqueue(hash string, content string, priority int) bool {
	w.mu.Lock()
	defer w.mu.Unlock()

	if len(w.warmupQueue) >= w.maxQueueSize {
		return false
	}
	w.warmupQueue = append(w.warmupQueue, WarmupTask{
		PromptHash:    hash,
		PromptContent: content,
		Priority:      priority,
	})
	return true
}

// Dequeue 取出优先级最高的任务
func (w *PromptCacheWarmer) Dequeue() (WarmupTask, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if len(w.warmupQueue) == 0 {
		return WarmupTask{}, false
	}

	w.warmupQueue = pcwSortQueue(w.warmupQueue)
	task := w.warmupQueue[0]
	w.warmupQueue = w.warmupQueue[1:]
	return task, true
}

// MarkWarmed 标记预热结果
func (w *PromptCacheWarmer) MarkWarmed(hash string, success bool) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.warmedPrompts[hash] = success
	if success {
		w.totalWarmed++
	} else {
		w.totalFailed++
	}
}

// IsWarmed 检查是否已预热
func (w *PromptCacheWarmer) IsWarmed(hash string) bool {
	w.mu.RLock()
	defer w.mu.RUnlock()

	return w.warmedPrompts[hash]
}

// GetStats 返回预热器统计信息
func (w *PromptCacheWarmer) GetStats() map[string]interface{} {
	w.mu.RLock()
	defer w.mu.RUnlock()

	return map[string]interface{}{
		"queueSize":    len(w.warmupQueue),
		"maxQueueSize": w.maxQueueSize,
		"warmedCount":  len(w.warmedPrompts),
		"totalWarmed":  w.totalWarmed,
		"totalFailed":  w.totalFailed,
	}
}

// Reset 重置预热器
func (w *PromptCacheWarmer) Reset() {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.warmupQueue = make([]WarmupTask, 0)
	w.warmedPrompts = make(map[string]bool)
	w.totalWarmed = 0
	w.totalFailed = 0
}

// pcwSortQueue 按优先级降序排序队列，返回排序后的新切片
func pcwSortQueue(queue []WarmupTask) []WarmupTask {
	result := make([]WarmupTask, len(queue))
	copy(result, queue)

	// 选择排序：按优先级降序
	for i := 0; i < len(result); i++ {
		maxIdx := i
		for j := i + 1; j < len(result); j++ {
			if result[j].Priority > result[maxIdx].Priority {
				maxIdx = j
			}
		}
		result[i], result[maxIdx] = result[maxIdx], result[i]
	}
	return result
}

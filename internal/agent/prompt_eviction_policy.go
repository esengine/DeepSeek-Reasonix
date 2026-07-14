package agent
import "sync"

// OPT-195: PromptEvictionPolicy / 提示驱逐策略器
// 基于多维度策略（LRU或优先级）决定哪些提示应该被驱逐。

// EvictionEntry 驱逐条目结构体
type EvictionEntry struct {
	Key        string
	Size       int
	LastAccess int
	Priority   int
}

// PromptEvictionPolicy 提示驱逐策略器，基于多维度策略决定哪些提示应该被驱逐
type PromptEvictionPolicy struct {
	mu           sync.RWMutex
	maxEntries   int
	entries      map[string]EvictionEntry
	evictedCount int
	policy       string
}

// NewPromptEvictionPolicy 创建一个新的提示驱逐策略器
// policy可选"lru"或"priority"
func NewPromptEvictionPolicy(maxEntries int, policy string) *PromptEvictionPolicy {
	return &PromptEvictionPolicy{
		maxEntries: maxEntries,
		entries:    make(map[string]EvictionEntry),
		policy:     policy,
	}
}

// Add 添加条目
func (p *PromptEvictionPolicy) Add(key string, size int, priority int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.entries[key] = EvictionEntry{
		Key:      key,
		Size:     size,
		Priority: priority,
	}
}

// Access 记录访问
func (p *PromptEvictionPolicy) Access(key string, currentTime int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	entry, ok := p.entries[key]
	if !ok {
		return
	}
	entry.LastAccess = currentTime
	p.entries[key] = entry
}

// Evict 根据策略驱逐一个条目，返回被驱逐的key
func (p *PromptEvictionPolicy) Evict() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.entries) == 0 {
		return ""
	}
	key := pepFindEvictionCandidate(p.entries, p.policy)
	if key == "" {
		return ""
	}
	delete(p.entries, key)
	p.evictedCount++
	return key
}

// GetStats 返回统计信息
func (p *PromptEvictionPolicy) GetStats() map[string]interface{} {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return map[string]interface{}{
		"maxEntries":   p.maxEntries,
		"entryCount":   len(p.entries),
		"evictedCount": p.evictedCount,
		"policy":       p.policy,
	}
}

// Reset 重置策略器
func (p *PromptEvictionPolicy) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.entries = make(map[string]EvictionEntry)
	p.evictedCount = 0
}

// pepFindEvictionCandidate 辅助函数，根据策略找到应被驱逐的候选条目key
func pepFindEvictionCandidate(entries map[string]EvictionEntry, policy string) string {
	var candidateKey string
	var candidate EvictionEntry
	first := true
	for k, v := range entries {
		if first {
			candidateKey = k
			candidate = v
			first = false
			continue
		}
		if policy == "priority" {
			// 优先级策略：选择优先级最低的条目
			if v.Priority < candidate.Priority {
				candidateKey = k
				candidate = v
			}
		} else {
			// LRU策略（默认）：选择最近访问时间最早的条目
			if v.LastAccess < candidate.LastAccess {
				candidateKey = k
				candidate = v
			}
		}
	}
	return candidateKey
}

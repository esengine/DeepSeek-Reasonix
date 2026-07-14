package agent

import (
	"sort"
	"sync"
)

// ── OPT-122: TokenAwareEvictor (Token 感知驱逐器) ──
// 根据 token 节省量决定驱逐哪些缓存条目，支持 LRU、LFU 和
// Size 三种驱逐策略。在 token 预算紧张时，优先驱逐能释放
// 最多 token 的条目，同时兼顾访问模式。
//
// 策略说明：
//   - lru:  驱逐最久未用的条目（LastUsed 最小）
//   - lfu:  驱逐最少使用的条目（AccessFrequency 最小）
//   - size: 驱逐 token 占用最大的条目（TokenSize 最大）

// EvictionCandidate 表示一个驱逐候选条目。
type EvictionCandidate struct {
	Key             string
	TokenSize       int
	LastUsed        int
	AccessFrequency int
}

// TokenAwareEvictor Token 感知驱逐器，根据 token 节省量决定驱逐哪些缓存条目。
type TokenAwareEvictor struct {
	mu               sync.RWMutex
	totalEvicted     int
	totalTokensFreed int
	evictionPolicy   string
	candidates       []EvictionCandidate
}

// NewTokenAwareEvictor 创建一个新的 Token 感知驱逐器实例。
// policy 可选值："lru"（最久未用）、"lfu"（最少使用）、"size"（最大 token 占用）。
func NewTokenAwareEvictor(policy string) *TokenAwareEvictor {
	return &TokenAwareEvictor{
		evictionPolicy: policy,
		candidates:     make([]EvictionCandidate, 0),
	}
}

// AddCandidate 添加一个驱逐候选条目。
func (e *TokenAwareEvictor) AddCandidate(key string, tokenSize int, lastUsed int, accessFrequency int) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.candidates = append(e.candidates, EvictionCandidate{
		Key:             key,
		TokenSize:       tokenSize,
		LastUsed:        lastUsed,
		AccessFrequency: accessFrequency,
	})
}

// SelectForEviction 根据驱逐策略选择指定数量的候选条目，但不实际移除。
// "lru" 选择最久未用的条目，"lfu" 选择最少使用的条目，"size" 选择 token 占用最大的条目。
func (e *TokenAwareEvictor) SelectForEviction(count int) []EvictionCandidate {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if count <= 0 || len(e.candidates) == 0 {
		return []EvictionCandidate{}
	}

	candidates := make([]EvictionCandidate, len(e.candidates))
	copy(candidates, e.candidates)

	taeSortByPolicy(candidates, e.evictionPolicy)

	if count > len(candidates) {
		count = len(candidates)
	}

	return candidates[:count]
}

// Evict 执行驱逐操作，移除指定数量的候选条目并返回释放的 token 数。
func (e *TokenAwareEvictor) Evict(count int) int {
	e.mu.Lock()
	defer e.mu.Unlock()

	if count <= 0 || len(e.candidates) == 0 {
		return 0
	}

	candidates := make([]EvictionCandidate, len(e.candidates))
	copy(candidates, e.candidates)

	taeSortByPolicy(candidates, e.evictionPolicy)

	if count > len(candidates) {
		count = len(candidates)
	}

	evictedKeys := make(map[string]bool, count)
	tokensFreed := 0
	for i := 0; i < count; i++ {
		tokensFreed += candidates[i].TokenSize
		evictedKeys[candidates[i].Key] = true
	}

	remaining := make([]EvictionCandidate, 0, len(e.candidates)-count)
	for _, c := range e.candidates {
		if !evictedKeys[c.Key] {
			remaining = append(remaining, c)
		}
	}
	e.candidates = remaining

	e.totalEvicted += count
	e.totalTokensFreed += tokensFreed

	return tokensFreed
}

// GetStats 返回驱逐器的统计信息。
func (e *TokenAwareEvictor) GetStats() map[string]interface{} {
	e.mu.RLock()
	defer e.mu.RUnlock()

	stats := make(map[string]interface{})
	stats["totalEvicted"] = e.totalEvicted
	stats["totalTokensFreed"] = e.totalTokensFreed
	stats["policy"] = e.evictionPolicy
	stats["candidateCount"] = len(e.candidates)

	return stats
}

// Reset 重置驱逐器的所有统计数据和候选列表。
func (e *TokenAwareEvictor) Reset() {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.totalEvicted = 0
	e.totalTokensFreed = 0
	e.candidates = make([]EvictionCandidate, 0)
}

// taeSortByPolicy 根据驱逐策略对候选条目进行排序。
func taeSortByPolicy(candidates []EvictionCandidate, policy string) {
	switch policy {
	case "lru":
		// 最久未用：LastUsed 最小的排在前面
		sort.Slice(candidates, func(i, j int) bool {
			return candidates[i].LastUsed < candidates[j].LastUsed
		})
	case "lfu":
		// 最少使用：AccessFrequency 最小的排在前面
		sort.Slice(candidates, func(i, j int) bool {
			return candidates[i].AccessFrequency < candidates[j].AccessFrequency
		})
	case "size":
		// 最大 token 占用：TokenSize 最大的排在前面
		sort.Slice(candidates, func(i, j int) bool {
			return candidates[i].TokenSize > candidates[j].TokenSize
		})
	}
}

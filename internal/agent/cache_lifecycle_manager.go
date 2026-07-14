package agent

import "sync"

// ── OPT-118: CacheLifecycleManager (缓存生命周期管理器) ──
// 管理缓存条目从创建到淘汰的完整生命周期，跟踪访问频率与年龄，
// 支持基于最大年龄的过期淘汰策略。
//
// 原理：每个缓存条目记录创建时间（CreatedAt）、最后访问时间（LastAccessed）
// 和访问计数（AccessCount）。EvictExpired 根据当前轮次与条目创建时间的
// 差值判断是否超过 maxAge，超过则淘汰。
//
// 效果：通过生命周期管理，避免过期缓存条目占用内存，同时统计淘汰
// 与过期数量，为缓存容量规划提供数据支撑。

// CacheLifecycleEntry 缓存生命周期条目
type CacheLifecycleEntry struct {
	Key          string
	CreatedAt    int
	LastAccessed int
	AccessCount  int
	Size         int
}

// CacheLifecycleManager 缓存生命周期管理器
type CacheLifecycleManager struct {
	mu           sync.RWMutex
	entries      map[string]*CacheLifecycleEntry
	totalCreated int
	totalEvicted int
	totalExpired int
	maxAge       int
}

// NewCacheLifecycleManager 创建缓存生命周期管理器。
// maxAge 指定条目的最大存活轮次，超过该值的条目将在 EvictExpired 时被淘汰。
func NewCacheLifecycleManager(maxAge int) *CacheLifecycleManager {
	return &CacheLifecycleManager{
		entries: make(map[string]*CacheLifecycleEntry),
		maxAge:  maxAge,
	}
}

// Create 创建一个新的缓存条目。
// 若 key 已存在则覆盖原有条目，并递增 totalCreated 计数。
func (m *CacheLifecycleManager) Create(key string, size int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.entries[key] = &CacheLifecycleEntry{
		Key:          key,
		CreatedAt:    0,
		LastAccessed: 0,
		AccessCount:  0,
		Size:         size,
	}
	m.totalCreated++
}

// Access 访问缓存条目，更新 LastAccessed 和 AccessCount。
// 若条目不存在则不做任何操作。
func (m *CacheLifecycleManager) Access(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	entry, ok := m.entries[key]
	if !ok {
		return
	}
	entry.AccessCount++
	entry.LastAccessed = entry.AccessCount
}

// EvictExpired 淘汰超过 maxAge 的条目。
// 条目年龄 = currentTurn - CreatedAt，超过 maxAge 则淘汰。
// 返回被淘汰的条目数量。
func (m *CacheLifecycleManager) EvictExpired(currentTurn int) int {
	m.mu.Lock()
	defer m.mu.Unlock()

	evicted := 0
	for key, entry := range m.entries {
		if clmIsExpired(entry, currentTurn, m.maxAge) {
			delete(m.entries, key)
			m.totalExpired++
			evicted++
		}
	}
	m.totalEvicted += evicted
	return evicted
}

// GetStats 返回生命周期管理器的统计信息，包括 totalCreated、
// totalEvicted、totalExpired、activeEntries 和 avgAccessCount。
func (m *CacheLifecycleManager) GetStats() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var totalAccess int
	for _, entry := range m.entries {
		totalAccess += entry.AccessCount
	}

	var avgAccessCount float64
	if len(m.entries) > 0 {
		avgAccessCount = float64(totalAccess) / float64(len(m.entries))
	}

	return map[string]interface{}{
		"totalCreated":   m.totalCreated,
		"totalEvicted":   m.totalEvicted,
		"totalExpired":   m.totalExpired,
		"activeEntries":  len(m.entries),
		"avgAccessCount": avgAccessCount,
	}
}

// Reset 重置管理器，清除所有条目与统计。
func (m *CacheLifecycleManager) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.entries = make(map[string]*CacheLifecycleEntry)
	m.totalCreated = 0
	m.totalEvicted = 0
	m.totalExpired = 0
}

// ---------------------------------------------------------------------------
// 内部辅助函数（以 clm 为前缀，避免命名冲突）
// ---------------------------------------------------------------------------

// clmEntryAge 计算条目年龄（当前轮次 - 创建轮次）。
func clmEntryAge(entry *CacheLifecycleEntry, currentTurn int) int {
	return currentTurn - entry.CreatedAt
}

// clmIsExpired 判断条目是否已过期（年龄超过 maxAge）。
func clmIsExpired(entry *CacheLifecycleEntry, currentTurn, maxAge int) bool {
	return clmEntryAge(entry, currentTurn) > maxAge
}

package agent
import "sync"

// ── OPT-230: PromptCacheLifecycleManager (提示缓存生命周期管理器) ──
// PromptCacheLifecycleManager 管理缓存项的完整生命周期，
// 包括创建、访问、刷新与过期。
//
// 注意：条目类型命名为 PromptCacheLifecycleEntry，以避免与
// OPT-118 (cache_lifecycle_manager.go) 中已存在的 CacheLifecycleEntry 冲突。
type PromptCacheLifecycleManager struct {
	mu             sync.RWMutex
	entries        map[string]PromptCacheLifecycleEntry
	createdCount   int
	expiredCount   int
	refreshedCount int
}

// PromptCacheLifecycleEntry 提示缓存生命周期条目。
type PromptCacheLifecycleEntry struct {
	Key            string
	CreatedAt      int64
	LastAccessedAt int64
	TTL            int64
	RefreshCount   int
}

// NewPromptCacheLifecycleManager 创建提示缓存生命周期管理器。
func NewPromptCacheLifecycleManager() *PromptCacheLifecycleManager {
	return &PromptCacheLifecycleManager{
		entries: make(map[string]PromptCacheLifecycleEntry),
	}
}

// Create 创建一个缓存项。
func (m *PromptCacheLifecycleManager) Create(key string, createdAt int64, ttl int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries[key] = PromptCacheLifecycleEntry{
		Key:            key,
		CreatedAt:      createdAt,
		LastAccessedAt: createdAt,
		TTL:            ttl,
		RefreshCount:   0,
	}
	m.createdCount++
}

// Access 访问缓存项，更新访问时间并返回是否仍然有效。
// 若 key 不存在返回 false。
func (m *PromptCacheLifecycleManager) Access(key string, accessedAt int64) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.entries[key]
	if !ok {
		return false
	}
	entry.LastAccessedAt = accessedAt
	m.entries[key] = entry
	return !pclmIsExpired(entry, accessedAt)
}

// Refresh 刷新缓存项的 TTL，返回是否刷新成功。
// 若 key 不存在返回 false。
func (m *PromptCacheLifecycleManager) Refresh(key string, newTTL int64) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.entries[key]
	if !ok {
		return false
	}
	entry.TTL = newTTL
	entry.RefreshCount++
	m.entries[key] = entry
	m.refreshedCount++
	return true
}

// Expire 过期所有超时项，返回被过期 key 的列表。
func (m *PromptCacheLifecycleManager) Expire(currentTime int64) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	expired := make([]string, 0)
	for key, entry := range m.entries {
		if pclmIsExpired(entry, currentTime) {
			expired = append(expired, key)
			delete(m.entries, key)
			m.expiredCount++
		}
	}
	return expired
}

// GetStats 返回生命周期管理器的统计信息。
func (m *PromptCacheLifecycleManager) GetStats() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return map[string]interface{}{
		"entryCount":     len(m.entries),
		"createdCount":   m.createdCount,
		"expiredCount":   m.expiredCount,
		"refreshedCount": m.refreshedCount,
	}
}

// Reset 重置生命周期管理器，清空所有缓存项与统计。
func (m *PromptCacheLifecycleManager) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries = make(map[string]PromptCacheLifecycleEntry)
	m.createdCount = 0
	m.expiredCount = 0
	m.refreshedCount = 0
}

// pclmIsExpired 判断缓存项是否已过期。
// 当 currentTime - CreatedAt > TTL 时视为过期。
func pclmIsExpired(entry PromptCacheLifecycleEntry, currentTime int64) bool {
	return currentTime-entry.CreatedAt > entry.TTL
}

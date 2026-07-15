package agent
import "sync"

// ── OPT-243: ContextWindowEvictionManager (上下文窗口驱逐管理器 / Context Window Eviction Manager) ──
// 管理上下文窗口中的条目驱逐，采用 LRU（最久未访问）策略释放空间。
// 当条目数超过 maxEntries 时自动驱逐最久未访问的条目。
// 支持手动驱逐与访问时间更新，累计已释放的 token 数。

// CWEMEntry 上下文窗口条目
type CWEMEntry struct {
	Key         string // 条目键
	Size        int    // 条目占用 token 数
	LastAccess  int64  // 最近访问时间戳
	AccessCount int    // 累计访问次数
}

// ContextWindowEvictionManager 上下文窗口驱逐管理器
type ContextWindowEvictionManager struct {
	mu               sync.RWMutex
	entries          map[string]CWEMEntry // 当前窗口条目
	maxEntries       int                  // 最大条目数
	evictedCount     int                  // 累计驱逐次数
	totalTokensFreed int                  // 累计释放的 token 数
}

// NewContextWindowEvictionManager 创建一个新的上下文窗口驱逐管理器实例。
// maxEntries 指定窗口最大条目数，超出时触发 LRU 驱逐。
func NewContextWindowEvictionManager(maxEntries int) *ContextWindowEvictionManager {
	return &ContextWindowEvictionManager{
		entries:    make(map[string]CWEMEntry),
		maxEntries: maxEntries,
	}
}

// Add 添加条目到上下文窗口。
// 当条目数达到 maxEntries 时，先驱逐最久未访问的条目再添加。
// 若 key 已存在则更新其大小。
func (c *ContextWindowEvictionManager) Add(key string, size int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.entries[key]; !exists {
		for len(c.entries) >= c.maxEntries {
			lruKey, ok := cwemFindLRU(c.entries)
			if !ok {
				break
			}
			freed := c.entries[lruKey].Size
			delete(c.entries, lruKey)
			c.evictedCount++
			c.totalTokensFreed += freed
		}
	}
	c.entries[key] = CWEMEntry{
		Key:         key,
		Size:        size,
		LastAccess:  0,
		AccessCount: 0,
	}
}

// Access 访问指定条目，更新其最近访问时间与访问次数。
// 若条目不存在则不做任何操作。
func (c *ContextWindowEvictionManager) Access(key string, timestamp int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if entry, ok := c.entries[key]; ok {
		entry.LastAccess = timestamp
		entry.AccessCount++
		c.entries[key] = entry
	}
}

// EvictOne 手动驱逐一个最久未访问的条目（LRU）。
// 返回被驱逐条目的 key 与是否成功；窗口为空时返回 ("", false)。
func (c *ContextWindowEvictionManager) EvictOne() (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	lruKey, ok := cwemFindLRU(c.entries)
	if !ok {
		return "", false
	}
	freed := c.entries[lruKey].Size
	delete(c.entries, lruKey)
	c.evictedCount++
	c.totalTokensFreed += freed
	return lruKey, true
}

// GetStats 获取统计信息。
// 返回 maxEntries、entryCount、evictedCount、totalTokensFreed。
func (c *ContextWindowEvictionManager) GetStats() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return map[string]interface{}{
		"maxEntries":       c.maxEntries,
		"entryCount":       len(c.entries),
		"evictedCount":     c.evictedCount,
		"totalTokensFreed": c.totalTokensFreed,
	}
}

// Reset 重置窗口条目与累计统计信息。
// 保留 maxEntries 配置。
func (c *ContextWindowEvictionManager) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = make(map[string]CWEMEntry)
	c.evictedCount = 0
	c.totalTokensFreed = 0
}

// cwemFindLRU 辅助函数，查找最久未访问（LastAccess 最小）的条目 key。
// 返回该 key 与是否找到；窗口为空时返回 ("", false)。
func cwemFindLRU(entries map[string]CWEMEntry) (string, bool) {
	if len(entries) == 0 {
		return "", false
	}
	var lruKey string
	var lruTime int64
	first := true
	for k, v := range entries {
		if first || v.LastAccess < lruTime {
			lruKey = k
			lruTime = v.LastAccess
			first = false
		}
	}
	return lruKey, true
}

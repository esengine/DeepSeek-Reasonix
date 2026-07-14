package agent
import "sync"

// OPT-172: CacheVersionManager — 缓存版本管理器
// CacheVersionManager manages versions of cache entries to support concurrent updates.
// It tracks per-key versions and supports transactional commit/rollback semantics.
type CacheVersionManager struct {
	mu             sync.RWMutex
	versions       map[string]int64 // key到版本的映射 key-to-version mapping
	globalVersion  int64            // 全局版本号 global version number
	rollbacks      int              // 回滚次数 number of rollbacks
	commits        int              // 提交次数 number of commits
}

// NewCacheVersionManager creates a new CacheVersionManager.
// NewCacheVersionManager 创建新的缓存版本管理器。
func NewCacheVersionManager() *CacheVersionManager {
	return &CacheVersionManager{
		versions:      make(map[string]int64),
		globalVersion: 0,
		rollbacks:     0,
		commits:       0,
	}
}

// BeginTransaction begins a transaction for the given key, returning the current version.
// BeginTransaction 为指定key开始事务，返回当前版本号。
func (c *CacheVersionManager) BeginTransaction(key string) int64 {
	c.mu.Lock()
	defer c.mu.Unlock()

	v, ok := c.versions[key]
	if !ok {
		v = 0
		c.versions[key] = 0
	}
	return v
}

// Commit commits the transaction for the given key, incrementing its version number.
// Commit 提交指定key的事务，递增其版本号。
func (c *CacheVersionManager) Commit(key string) int64 {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.versions[key]++
	c.globalVersion++
	c.commits++
	return c.versions[key]
}

// Rollback rolls back the transaction for the given key, restoring the previous version.
// Rollback 回滚指定key的事务，恢复版本号。
func (c *CacheVersionManager) Rollback(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if v, ok := c.versions[key]; ok && v > 0 {
		c.versions[key]--
	}
	c.rollbacks++
}

// GetVersion returns the current version for the given key.
// GetVersion 返回指定key的当前版本号。
func (c *CacheVersionManager) GetVersion(key string) int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return cvmGetVersion(c.versions, key)
}

// GetStats returns statistics about the version manager.
// GetStats 返回版本管理器的统计信息。
func (c *CacheVersionManager) GetStats() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return map[string]interface{}{
		"trackedKeys":     len(c.versions),
		"globalVersion":   c.globalVersion,
		"commits":         c.commits,
		"rollbacks":       c.rollbacks,
	}
}

// Reset resets the version manager to its initial state.
// Reset 将版本管理器重置为初始状态。
func (c *CacheVersionManager) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.versions = make(map[string]int64)
	c.globalVersion = 0
	c.rollbacks = 0
	c.commits = 0
}

// cvmGetVersion retrieves the version for a key from the versions map (helper).
// cvmGetVersion 从版本映射中获取指定key的版本号（辅助函数）。
func cvmGetVersion(versions map[string]int64, key string) int64 {
	if v, ok := versions[key]; ok {
		return v
	}
	return 0
}

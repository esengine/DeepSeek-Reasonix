package agent
import "sync"

// OPT-192: CacheFreshnessGuarantor / 缓存新鲜度保证器
// 保证缓存内容在有效期内，支持过期检查和批量清理。

// CacheFreshnessGuarantor 缓存新鲜度保证器，保证缓存内容在有效期内
type CacheFreshnessGuarantor struct {
	mu           sync.RWMutex
	entries      map[string]int64 // key->expiryTimestamp
	maxAge       int
	staleCount   int
	checkedCount int
	logicalClock int64 // 内部逻辑时钟，用于Put时设置过期时间
}

// NewCacheFreshnessGuarantor 创建一个新的缓存新鲜度保证器
func NewCacheFreshnessGuarantor(maxAge int) *CacheFreshnessGuarantor {
	return &CacheFreshnessGuarantor{
		entries: make(map[string]int64),
		maxAge:  maxAge,
	}
}

// Put 放入缓存项，设置过期时间（当前逻辑时间+maxAge）
func (g *CacheFreshnessGuarantor) Put(key string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.logicalClock++
	g.entries[key] = g.logicalClock + int64(g.maxAge)
}

// IsFresh 检查缓存项是否在有效期内
func (g *CacheFreshnessGuarantor) IsFresh(key string, currentTime int64) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.checkedCount++
	expiry, ok := g.entries[key]
	if !ok {
		return false
	}
	return expiry > currentTime
}

// RemoveStale 移除过期项，返回移除数量
func (g *CacheFreshnessGuarantor) RemoveStale(currentTime int64) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	removed := 0
	for key, expiry := range g.entries {
		if expiry <= currentTime {
			delete(g.entries, key)
			removed++
		}
	}
	g.staleCount += removed
	return removed
}

// GetStats 返回统计信息，包括 entryCount, maxAge, staleCount, checkedCount
func (g *CacheFreshnessGuarantor) GetStats() map[string]interface{} {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return map[string]interface{}{
		"entryCount":   len(g.entries),
		"maxAge":       g.maxAge,
		"staleCount":   g.staleCount,
		"checkedCount": g.checkedCount,
	}
}

// Reset 重置保证器，清空所有缓存项和统计
func (g *CacheFreshnessGuarantor) Reset() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.entries = make(map[string]int64)
	g.staleCount = 0
	g.checkedCount = 0
	g.logicalClock = 0
}

// cfgCountFresh 辅助函数，计算在指定时间点仍新鲜的条目数量
func cfgCountFresh(entries map[string]int64, currentTime int64) int {
	count := 0
	for _, expiry := range entries {
		if expiry > currentTime {
			count++
		}
	}
	return count
}

package agent

import "sync"

// OPT-247: CacheInvalidationWave / 缓存失效波
// 以波浪方式批量传播缓存失效，逐波收集key并推进传播。
type CIWave struct {
	Keys      []string
	Timestamp int64
}

// CacheInvalidationWave 缓存失效波管理器。
type CacheInvalidationWave struct {
	mu               sync.RWMutex
	waves            []CIWave
	currentWaveIndex int
	totalPropagated  int
	waveCount        int
}

// NewCacheInvalidationWave 创建一个新的缓存失效波管理器。
func NewCacheInvalidationWave() *CacheInvalidationWave {
	return &CacheInvalidationWave{
		waves:            make([]CIWave, 0),
		currentWaveIndex: -1,
	}
}

// StartWave 开始一个新的失效波，记录时间戳并将当前波指针指向新波。
func (c *CacheInvalidationWave) StartWave(timestamp int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.waves = append(c.waves, CIWave{
		Keys:      make([]string, 0),
		Timestamp: timestamp,
	})
	c.currentWaveIndex = len(c.waves) - 1
	c.waveCount++
}

// AddToWave 将key添加到当前波，返回是否成功。
// 若当前不存在有效波则返回false。
func (c *CacheInvalidationWave) AddToWave(key string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.currentWaveIndex < 0 || c.currentWaveIndex >= len(c.waves) {
		return false
	}
	c.waves[c.currentWaveIndex].Keys = append(c.waves[c.currentWaveIndex].Keys, key)
	return true
}

// PropagateWave 传播当前波，返回当前波的所有key并将指针推进到下一波。
// 若当前不存在有效波则返回nil。
func (c *CacheInvalidationWave) PropagateWave() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.currentWaveIndex < 0 || c.currentWaveIndex >= len(c.waves) {
		return nil
	}
	keys := ciwCopyKeys(c.waves[c.currentWaveIndex].Keys)
	c.totalPropagated += len(keys)
	c.currentWaveIndex++
	return keys
}

// GetStats 获取统计信息，包含 waveCount、currentWaveSize、totalPropagated、totalWaves。
func (c *CacheInvalidationWave) GetStats() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()
	currentWaveSize := 0
	if c.currentWaveIndex >= 0 && c.currentWaveIndex < len(c.waves) {
		currentWaveSize = len(c.waves[c.currentWaveIndex].Keys)
	}
	return map[string]interface{}{
		"waveCount":       c.waveCount,
		"currentWaveSize": currentWaveSize,
		"totalPropagated": c.totalPropagated,
		"totalWaves":      len(c.waves),
	}
}

// Reset 重置所有状态，清空波列表与计数器。
func (c *CacheInvalidationWave) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.waves = make([]CIWave, 0)
	c.currentWaveIndex = -1
	c.totalPropagated = 0
	c.waveCount = 0
}

// ciwCopyKeys 复制一个string切片，避免外部修改影响内部数据（辅助函数）。
func ciwCopyKeys(keys []string) []string {
	cp := make([]string, len(keys))
	copy(cp, keys)
	return cp
}

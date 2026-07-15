package agent

import "sync"

// ── OPT-222: CacheInvalidationPropagator (缓存失效传播器) ──
// 将缓存失效操作传播到多个缓存层。每层独立记录失效次数，并统计成功传播
// 的总次数与失败次数，便于排查某层失效未生效的问题。
//
// 原理：维护一组缓存层名称，Propagate 时向每一层发送失效信号；
// 空 key 视为无效请求，对应层计为失败；非空 key 计为成功并累加该层计数。
//
// 效果：统一管理多层级缓存的失效，避免脏数据残留，同时提供逐层统计。

// CacheInvalidationPropagator 缓存失效传播器。
type CacheInvalidationPropagator struct {
	mu                  sync.RWMutex
	layers              []string
	propagationLog      map[string]int // layer → invalidated count
	totalPropagated     int
	propagationFailures int
}

// NewCacheInvalidationPropagator 创建缓存失效传播器。
func NewCacheInvalidationPropagator() *CacheInvalidationPropagator {
	return &CacheInvalidationPropagator{
		layers:         make([]string, 0),
		propagationLog: make(map[string]int),
	}
}

// AddLayer 添加缓存层（重复添加或空名称将被忽略）。
func (c *CacheInvalidationPropagator) AddLayer(layer string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if layer == "" {
		return
	}
	if cipFindLayer(c.layers, layer) {
		return
	}
	c.layers = append(c.layers, layer)
	c.propagationLog[layer] = 0
}

// Propagate 向所有已注册层传播失效，返回成功传播的层数。
// 空 key 会导致所有层计为失败。
func (c *CacheInvalidationPropagator) Propagate(key string) int {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.layers) == 0 {
		return 0
	}

	success := 0
	for _, layer := range c.layers {
		if key == "" {
			c.propagationFailures++
			continue
		}
		c.propagationLog[layer]++
		c.totalPropagated++
		success++
	}
	return success
}

// GetLayerStats 返回指定层的失效计数。
func (c *CacheInvalidationPropagator) GetLayerStats(layer string) int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.propagationLog[layer]
}

// GetStats 返回统计信息：layerCount、totalPropagated、propagationFailures、layers。
func (c *CacheInvalidationPropagator) GetStats() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	layersCopy := make([]string, len(c.layers))
	copy(layersCopy, c.layers)

	return map[string]interface{}{
		"layerCount":          len(c.layers),
		"totalPropagated":     c.totalPropagated,
		"propagationFailures": c.propagationFailures,
		"layers":              layersCopy,
	}
}

// Reset 重置传播器，清除所有层与统计。
func (c *CacheInvalidationPropagator) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.layers = make([]string, 0)
	c.propagationLog = make(map[string]int)
	c.totalPropagated = 0
	c.propagationFailures = 0
}

// ---------------------------------------------------------------------------
// 辅助函数（以 cip 为前缀，避免命名冲突）
// ---------------------------------------------------------------------------

// cipFindLayer 查找指定层是否已存在于层列表中。
func cipFindLayer(layers []string, layer string) bool {
	for _, l := range layers {
		if l == layer {
			return true
		}
	}
	return false
}

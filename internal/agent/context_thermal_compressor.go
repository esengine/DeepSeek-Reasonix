package agent
import "sync"

// ── OPT-228: ContextThermalCompressor (上下文热力压缩器) ──
// ContextThermalCompressor 基于访问热度压缩上下文，
// 移除访问次数低于阈值的低热度片段。
type ContextThermalCompressor struct {
	mu              sync.RWMutex
	heatMap         map[string]int // fragment -> accessCount
	compressedCount int
	totalFreed      int
	threshold       int
}

// NewContextThermalCompressor 创建上下文热力压缩器。
// threshold 为热度阈值，访问次数低于该值的片段将被压缩移除。
func NewContextThermalCompressor(threshold int) *ContextThermalCompressor {
	return &ContextThermalCompressor{
		heatMap:   make(map[string]int),
		threshold: threshold,
	}
}

// RecordAccess 记录片段访问，增加其热度计数。
func (c *ContextThermalCompressor) RecordAccess(fragment string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.heatMap[fragment]++
}

// GetHeat 获取指定片段的访问热度（访问次数）。
func (c *ContextThermalCompressor) GetHeat(fragment string) int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.heatMap[fragment]
}

// Compress 压缩低热度片段（访问次数低于 threshold 的移除），
// 返回保留的（高热度）片段列表。
func (c *ContextThermalCompressor) Compress(fragments []string) []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	kept := ctcFilterByHeat(c.heatMap, fragments, c.threshold)
	freed := len(fragments) - len(kept)
	if freed > 0 {
		c.compressedCount++
		c.totalFreed += freed
	}
	return kept
}

// GetStats 返回热力压缩器的统计信息。
func (c *ContextThermalCompressor) GetStats() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return map[string]interface{}{
		"trackedFragments": len(c.heatMap),
		"compressedCount":  c.compressedCount,
		"totalFreed":       c.totalFreed,
		"threshold":        c.threshold,
	}
}

// Reset 重置热力压缩器，清空热度图与统计，保留阈值配置。
func (c *ContextThermalCompressor) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.heatMap = make(map[string]int)
	c.compressedCount = 0
	c.totalFreed = 0
}

// ctcFilterByHeat 按热度过滤片段，保留热度不低于阈值的片段。
func ctcFilterByHeat(heatMap map[string]int, fragments []string, threshold int) []string {
	kept := make([]string, 0, len(fragments))
	for _, f := range fragments {
		if heatMap[f] >= threshold {
			kept = append(kept, f)
		}
	}
	return kept
}

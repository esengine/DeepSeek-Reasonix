package agent
import "sync"

// ── OPT-245: PromptCachePressureIndex (提示缓存压力指数 / Prompt Cache Pressure Index) ──
// 综合评估缓存系统的压力水平，基于命中率、未命中率和驱逐率计算压力指数。
// 压力指数 = missRate*0.5 + evictionRate*0.5 - hitRate*0.2，范围 0~1。
// 根据压力指数划分四个等级：low / medium / high / critical。

// PromptCachePressureIndex 提示缓存压力指数
type PromptCachePressureIndex struct {
	mu            sync.RWMutex
	hitRate       float64 // 命中率
	missRate      float64 // 未命中率
	evictionRate  float64 // 驱逐率
	pressureIndex float64 // 压力指数(0~1)
	calculations  int     // 计算次数
}

// NewPromptCachePressureIndex 创建一个新的提示缓存压力指数实例。
func NewPromptCachePressureIndex() *PromptCachePressureIndex {
	return &PromptCachePressureIndex{}
}

// Update 更新命中率、未命中率和驱逐率，并重新计算压力指数。
// 每次调用累加计算次数。
func (p *PromptCachePressureIndex) Update(hitRate float64, missRate float64, evictionRate float64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.hitRate = hitRate
	p.missRate = missRate
	p.evictionRate = evictionRate
	p.pressureIndex = pcpiComputePressure(hitRate, missRate, evictionRate)
	p.calculations++
}

// GetPressureLevel 获取压力等级（low/medium/high/critical）。
func (p *PromptCachePressureIndex) GetPressureLevel() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return pcpiLevelFromIndex(p.pressureIndex)
}

// GetPressureIndex 获取压力指数(0~1)。
func (p *PromptCachePressureIndex) GetPressureIndex() float64 {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.pressureIndex
}

// GetStats 获取统计信息。
// 返回 hitRate、missRate、evictionRate、pressureIndex、pressureLevel、calculations。
func (p *PromptCachePressureIndex) GetStats() map[string]interface{} {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return map[string]interface{}{
		"hitRate":       p.hitRate,
		"missRate":      p.missRate,
		"evictionRate":  p.evictionRate,
		"pressureIndex": p.pressureIndex,
		"pressureLevel": pcpiLevelFromIndex(p.pressureIndex),
		"calculations":  p.calculations,
	}
}

// Reset 重置所有指标与累计统计信息。
func (p *PromptCachePressureIndex) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.hitRate = 0
	p.missRate = 0
	p.evictionRate = 0
	p.pressureIndex = 0
	p.calculations = 0
}

// pcpiComputePressure 辅助函数，根据指标计算压力指数(0~1)。
// 压力指数 = missRate*0.5 + evictionRate*0.5 - hitRate*0.2，
// 结果裁剪到 [0, 1] 区间。
func pcpiComputePressure(hitRate float64, missRate float64, evictionRate float64) float64 {
	index := missRate*0.5 + evictionRate*0.5 - hitRate*0.2
	if index < 0 {
		index = 0
	}
	if index > 1 {
		index = 1
	}
	return index
}

// pcpiLevelFromIndex 辅助函数，根据压力指数返回压力等级。
// >=0.8 为 critical，>=0.6 为 high，>=0.3 为 medium，其余为 low。
func pcpiLevelFromIndex(index float64) string {
	switch {
	case index >= 0.8:
		return "critical"
	case index >= 0.6:
		return "high"
	case index >= 0.3:
		return "medium"
	default:
		return "low"
	}
}

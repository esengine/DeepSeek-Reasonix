package agent

import "sync"

// ── OPT-187: CacheWarmingOptimizer (缓存预热优化器) ──
// 优化缓存预热策略以最大化缓存命中率。记录每次预热的键及预热后
// 是否命中，统计命中改善率，并判断特定键是否需要进一步优化预热策略。
//
// 原理：缓存预热是在请求到来前提前加载热点数据的过程。并非所有
// 预热都能带来命中提升——通过追踪预热后的实际命中情况，可以识别
// 出低效预热并进行策略调整。
//
// 效果：量化预热效果，指导预热策略迭代，提升缓存命中率，
// 减少无效预热带来的资源浪费。

// WarmupResult 预热结果记录。
type WarmupResult struct {
	Key            string // 缓存键
	Warmed         bool   // 是否已执行预热
	HitAfterWarmup bool   // 预热后是否命中
}

// CacheWarmingOptimizer 缓存预热优化器，优化预热策略以最大化缓存命中率。
type CacheWarmingOptimizer struct {
	mu                  sync.RWMutex
	warmupHistory       map[string]WarmupResult
	optimizedCount      int
	totalHitImprovement float64
}

// NewCacheWarmingOptimizer 创建一个新的缓存预热优化器。
func NewCacheWarmingOptimizer() *CacheWarmingOptimizer {
	return &CacheWarmingOptimizer{
		warmupHistory:       make(map[string]WarmupResult),
		optimizedCount:      0,
		totalHitImprovement: 0,
	}
}

// RecordWarmup 记录预热结果。
// key 为被预热的缓存键，hitAfterWarmup 指示预热后是否发生了缓存命中。
// 若此前记录该键未命中而本次命中，则累加命中改善。
func (c *CacheWarmingOptimizer) RecordWarmup(key string, hitAfterWarmup bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	prev, existed := c.warmupHistory[key]
	c.warmupHistory[key] = WarmupResult{
		Key:            key,
		Warmed:         true,
		HitAfterWarmup: hitAfterWarmup,
	}
	if existed {
		// 之前未命中而现在命中，记录一次改善
		if !prev.HitAfterWarmup && hitAfterWarmup {
			c.totalHitImprovement += 1.0
		}
	} else {
		// 首次预热即命中，也记录一次改善
		if hitAfterWarmup {
			c.totalHitImprovement += 1.0
		}
	}
	c.optimizedCount++
}

// GetWarmupEffectiveness 获取预热效果（命中改善率）。
// 若该键预热后命中则返回 1.0，否则返回 0.0；键不存在时返回 0.0。
func (c *CacheWarmingOptimizer) GetWarmupEffectiveness(key string) float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result, ok := c.warmupHistory[key]
	if !ok {
		return 0
	}
	if result.HitAfterWarmup {
		return 1.0
	}
	return 0.0
}

// ShouldOptimize 判断是否需要优化预热策略。
// 若键无预热记录或预热后未命中，则返回 true（需要优化）。
func (c *CacheWarmingOptimizer) ShouldOptimize(key string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result, ok := c.warmupHistory[key]
	if !ok {
		return true
	}
	return !result.HitAfterWarmup
}

// GetStats 返回统计信息，包含 trackedKeys、optimizedCount、
// totalHitImprovement 和 avgHitImprovement。
func (c *CacheWarmingOptimizer) GetStats() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()
	avg := cwoComputeAvg(c.totalHitImprovement, c.optimizedCount)
	return map[string]interface{}{
		"trackedKeys":         len(c.warmupHistory),
		"optimizedCount":      c.optimizedCount,
		"totalHitImprovement": c.totalHitImprovement,
		"avgHitImprovement":   avg,
	}
}

// Reset 重置优化器状态，清空预热历史和所有统计计数。
func (c *CacheWarmingOptimizer) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.warmupHistory = make(map[string]WarmupResult)
	c.optimizedCount = 0
	c.totalHitImprovement = 0
}

// cwoComputeAvg 计算平均命中改善率（辅助函数）。
// total 为累计改善值，count 为优化次数；count 为 0 时返回 0。
func cwoComputeAvg(total float64, count int) float64 {
	if count == 0 {
		return 0
	}
	return total / float64(count)
}

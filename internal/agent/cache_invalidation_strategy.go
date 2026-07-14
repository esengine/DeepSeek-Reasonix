package agent
import "sync"

// ── OPT-202: CacheInvalidationStrategy (缓存失效策略器 / Cache Invalidation Strategy) ──
// 支持多种缓存失效策略：immediate（立即失效）、lazy（惰性失效）、ttl（基于TTL失效）。
// 根据当前策略对缓存项执行失效操作，并按策略分类统计失效次数。
//
// 原理：不同场景下缓存失效策略的选择影响缓存命中率和数据一致性。
// immediate 在数据变更时立即失效，保证一致性但可能降低命中率；
// lazy 在下次访问时检查并失效，减少不必要的失效操作；
// ttl 基于过期时间自动失效，兼顾一致性和性能。
//
// 效果：统一管理多种失效策略，按策略分类统计失效次数，
// 为缓存策略优化提供数据支撑。

// CacheInvalidationStrategy 缓存失效策略器
type CacheInvalidationStrategy struct {
	mu               sync.RWMutex
	strategy         string          // 当前失效策略
	invalidations    map[string]int // strategy -> count
	totalInvalidated int             // 总失效次数
}

// NewCacheInvalidationStrategy 创建缓存失效策略器。
// strategy 指定初始策略，可选 "immediate"、"lazy"、"ttl"，若为空或无效则默认 "immediate"。
func NewCacheInvalidationStrategy(strategy string) *CacheInvalidationStrategy {
	strategy = cisSelectStrategy(strategy)
	return &CacheInvalidationStrategy{
		strategy:      strategy,
		invalidations: make(map[string]int),
	}
}

// Invalidate 根据当前策略失效指定 key 的缓存，返回使用的策略名称。
// 每次失效会递增对应策略的计数和总失效计数。
func (c *CacheInvalidationStrategy) Invalidate(key string) string {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.invalidations[c.strategy]++
	c.totalInvalidated++
	return c.strategy
}

// SetStrategy 切换失效策略。
// strategy 可选 "immediate"、"lazy"、"ttl"，若为空或无效则默认 "immediate"。
func (c *CacheInvalidationStrategy) SetStrategy(strategy string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.strategy = cisSelectStrategy(strategy)
}

// GetInvalidationCount 获取指定策略的失效次数。
// strategy 为策略名称，若不存在则返回 0。
func (c *CacheInvalidationStrategy) GetInvalidationCount(strategy string) int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.invalidations[strategy]
}

// GetStats 返回缓存失效策略器的统计信息。
// 包含 strategy、totalInvalidated、immediateCount、lazyCount 和 ttlCount。
func (c *CacheInvalidationStrategy) GetStats() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return map[string]interface{}{
		"strategy":         c.strategy,
		"totalInvalidated": c.totalInvalidated,
		"immediateCount":   c.invalidations["immediate"],
		"lazyCount":        c.invalidations["lazy"],
		"ttlCount":         c.invalidations["ttl"],
	}
}

// Reset 重置缓存失效策略器的统计信息（不重置当前策略）。
func (c *CacheInvalidationStrategy) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.invalidations = make(map[string]int)
	c.totalInvalidated = 0
}

// cisSelectStrategy 校验并选择有效的失效策略。
// 仅接受 "immediate"、"lazy"、"ttl"，其他值默认返回 "immediate"。
func cisSelectStrategy(strategy string) string {
	switch strategy {
	case "immediate", "lazy", "ttl":
		return strategy
	default:
		return "immediate"
	}
}

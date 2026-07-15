package agent
import "sync"

// ── OPT-235: PromptCacheWarmthTracker (提示缓存温度追踪器 / Prompt Cache Warmth Tracker) ──
// 追踪缓存的"温度"（热度）变化。通过 Warm 和 Cool 操作调整 key 的温度，
// 温度超过阈值的 key 被视为"热"缓存，适合保留在缓存中。
// 帮助识别高频访问的热点缓存项，优化缓存预热和驱逐策略。

// PromptCacheWarmthTracker 提示缓存温度追踪器
type PromptCacheWarmthTracker struct {
	mu          sync.RWMutex
	warmth      map[string]int // key -> 温度值
	totalChecks int            // 总操作次数（Warm + Cool）
	warmedCount int            // Warm 操作次数
	cooledCount int            // Cool 操作次数
	threshold   int            // 热度阈值，超过则视为"热"
}

// NewPromptCacheWarmthTracker 创建一个新的提示缓存温度追踪器。
// threshold 为热度阈值，温度超过该值的 key 被视为"热"缓存。
func NewPromptCacheWarmthTracker(threshold int) *PromptCacheWarmthTracker {
	return &PromptCacheWarmthTracker{
		warmth:    make(map[string]int),
		threshold: threshold,
	}
}

// Warm 增加指定 key 的温度（温度 +1）。
func (t *PromptCacheWarmthTracker) Warm(key string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.warmth[key]++
	t.warmedCount++
	t.totalChecks++
}

// Cool 降低指定 key 的温度（温度 -1，不低于 0）。
func (t *PromptCacheWarmthTracker) Cool(key string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.warmth[key] > 0 {
		t.warmth[key]--
	}
	t.cooledCount++
	t.totalChecks++
}

// GetWarmth 获取指定 key 的当前温度值。
func (t *PromptCacheWarmthTracker) GetWarmth(key string) int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.warmth[key]
}

// IsWarm 判断指定 key 的温度是否超过阈值。
func (t *PromptCacheWarmthTracker) IsWarm(key string) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.warmth[key] > t.threshold
}

// GetStats 返回温度追踪器的统计信息。
// 包含 trackedKeys、warmedCount、cooledCount、threshold、warmKeyCount。
func (t *PromptCacheWarmthTracker) GetStats() map[string]interface{} {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return map[string]interface{}{
		"trackedKeys":  len(t.warmth),
		"warmedCount":  t.warmedCount,
		"cooledCount":  t.cooledCount,
		"threshold":    t.threshold,
		"warmKeyCount": pcwtCountWarm(t.warmth, t.threshold),
	}
}

// Reset 重置温度追踪器状态（清空温度图和统计，保留 threshold 配置）。
func (t *PromptCacheWarmthTracker) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.warmth = make(map[string]int)
	t.totalChecks = 0
	t.warmedCount = 0
	t.cooledCount = 0
}

// pcwtCountWarm 统计温度超过阈值的 key 数量（辅助函数）。
func pcwtCountWarm(warmth map[string]int, threshold int) int {
	count := 0
	for _, w := range warmth {
		if w > threshold {
			count++
		}
	}
	return count
}

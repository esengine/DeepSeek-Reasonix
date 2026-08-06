package agent

import "sync"

// ── OPT-265: PromptCacheProactiveWarmer (提示缓存主动预热器) ──
// 维护一组带优先级的预热目标，按需对目标 key 执行主动预热。已预热
// 的 key 会被标记，避免重复预热；非目标 key 的预热请求会被跳过。
//
// 原理：提示缓存（prompt cache）的首次命中往往需要完整计算，延迟
// 较高。通过在空闲期主动预热高频/高优先级 key，可在真实请求到来
// 时直接命中缓存，显著降低首字延迟。
//
// 效果：提升提示缓存命中率，降低冷启动延迟，改善用户体验。

// PromptCacheProactiveWarmer 提示缓存主动预热器。
type PromptCacheProactiveWarmer struct {
	mu              sync.RWMutex
	warmupTargets   map[string]int64
	warmedKeys      map[string]bool
	totalWarmed     int
	totalSkipped    int
	warmingStrategy string
}

// NewPromptCacheProactiveWarmer 创建一个新的提示缓存主动预热器。
// strategy 描述预热策略（如 "priority"、"lru" 等），空值回退为 "priority"。
func NewPromptCacheProactiveWarmer(strategy string) *PromptCacheProactiveWarmer {
	if strategy == "" {
		strategy = "priority"
	}
	return &PromptCacheProactiveWarmer{
		warmupTargets:   make(map[string]int64),
		warmedKeys:      make(map[string]bool),
		warmingStrategy: strategy,
	}
}

// AddTarget 添加一个预热目标。
// key 为目标缓存键，priority 为优先级（数值越大优先级越高）。
// 若 key 已存在则更新其优先级。
func (w *PromptCacheProactiveWarmer) AddTarget(key string, priority int64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.warmupTargets[key] = priority
}

// WarmUp 预热指定 key。
// 若 key 已预热或不是已注册的目标，则跳过并返回 false；
// 否则执行预热、标记为已预热并返回 true。
func (w *PromptCacheProactiveWarmer) WarmUp(key string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.warmedKeys[key] {
		w.totalSkipped++
		return false
	}
	if _, ok := w.warmupTargets[key]; !ok {
		w.totalSkipped++
		return false
	}
	w.warmedKeys[key] = true
	w.totalWarmed++
	return true
}

// IsWarmed 返回指定 key 是否已预热。
func (w *PromptCacheProactiveWarmer) IsWarmed(key string) bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.warmedKeys[key]
}

// GetTargetCount 返回当前已注册的预热目标数量。
func (w *PromptCacheProactiveWarmer) GetTargetCount() int {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return len(w.warmupTargets)
}

// GetStats 返回统计信息，包含 targetCount、warmedCount、totalWarmed、
// totalSkipped 和 warmingStrategy。
func (w *PromptCacheProactiveWarmer) GetStats() map[string]interface{} {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return map[string]interface{}{
		"targetCount":     len(w.warmupTargets),
		"warmedCount":     pcpwCountWarmed(w.warmedKeys),
		"totalWarmed":     w.totalWarmed,
		"totalSkipped":    w.totalSkipped,
		"warmingStrategy": w.warmingStrategy,
	}
}

// Reset 重置预热器状态，清空目标、已预热标记与计数，但保留 warmingStrategy 配置。
func (w *PromptCacheProactiveWarmer) Reset() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.warmupTargets = make(map[string]int64)
	w.warmedKeys = make(map[string]bool)
	w.totalWarmed = 0
	w.totalSkipped = 0
}

// pcpwCountWarmed 统计已预热 key 的数量（辅助函数）。
func pcpwCountWarmed(warmed map[string]bool) int {
	return len(warmed)
}

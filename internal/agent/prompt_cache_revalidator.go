package agent
import "sync"

// ── OPT-205: PromptCacheRevalidator (提示缓存重新验证器 / Prompt Cache Revalidator) ──
// 定期重新验证缓存项的有效性，确保缓存数据不会过时。
// 每个缓存项注册后记录最后验证时间，超过重新验证间隔后标记为需要重新验证。
//
// 原理：缓存项可能因为底层数据变更而失效，定期重新验证可以及时发现失效。
// 重新验证间隔控制验证频率：间隔越短，数据越新鲜，但验证开销越大。
//
// 效果：在缓存一致性和性能间取得平衡，
// 统计重新验证次数、失效次数和总检查次数，为缓存管理提供数据支撑。

// PromptCacheRevalidator 提示缓存重新验证器
type PromptCacheRevalidator struct {
	mu                  sync.RWMutex
	entries             map[string]int64 // key -> lastValidated timestamp
	revalidationInterval int              // 重新验证间隔
	revalidatedCount    int              // 重新验证次数
	invalidatedCount    int              // 失效次数
	totalChecks         int              // 总检查次数
}

// NewPromptCacheRevalidator 创建提示缓存重新验证器。
// revalidationInterval 指定重新验证间隔（时间单位），若 <= 0 则默认 1000。
func NewPromptCacheRevalidator(revalidationInterval int) *PromptCacheRevalidator {
	if revalidationInterval <= 0 {
		revalidationInterval = 1000
	}
	return &PromptCacheRevalidator{
		entries:              make(map[string]int64),
		revalidationInterval: revalidationInterval,
	}
}

// Register 注册一个缓存项。
// key 为缓存项的标识，初始验证时间设为 0 表示尚未验证。
func (r *PromptCacheRevalidator) Register(key string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries[key] = 0
}

// NeedsRevalidation 检查指定 key 是否需要重新验证。
// 若 key 不存在或尚未验证过（时间戳为 0），则返回 true；
// 若自上次验证以来已超过重新验证间隔，则返回 true；否则返回 false。
func (r *PromptCacheRevalidator) NeedsRevalidation(key string, currentTime int64) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	lastValidated, ok := r.entries[key]
	if !ok {
		return true
	}
	if lastValidated == 0 {
		return true
	}
	return currentTime-lastValidated >= int64(r.revalidationInterval)
}

// Revalidate 执行重新验证。
// 若 stillValid 为 true，更新最后验证时间并递增 revalidatedCount，返回 true；
// 若 stillValid 为 false，递增 invalidatedCount 并从 entries 中移除该 key，返回 false。
// key 为缓存项标识，currentTime 为当前时间戳，stillValid 表示缓存项是否仍然有效。
func (r *PromptCacheRevalidator) Revalidate(key string, currentTime int64, stillValid bool) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.totalChecks++
	if stillValid {
		r.entries[key] = currentTime
		r.revalidatedCount++
		return true
	}
	delete(r.entries, key)
	r.invalidatedCount++
	return false
}

// GetStats 返回重新验证器的统计信息。
// 包含 entryCount、revalidationInterval、revalidatedCount、invalidatedCount 和 totalChecks。
func (r *PromptCacheRevalidator) GetStats() map[string]interface{} {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return map[string]interface{}{
		"entryCount":           len(r.entries),
		"revalidationInterval": r.revalidationInterval,
		"revalidatedCount":     r.revalidatedCount,
		"invalidatedCount":     r.invalidatedCount,
		"totalChecks":          r.totalChecks,
	}
}

// Reset 重置重新验证器的所有状态和计数。
// 清空所有缓存项，重置计数，但不重置重新验证间隔。
func (r *PromptCacheRevalidator) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries = make(map[string]int64)
	r.revalidatedCount = 0
	r.invalidatedCount = 0
	r.totalChecks = 0
}

// pcrFindStaleKeys 查找所有需要重新验证的缓存 key。
// entries 为缓存项映射（key -> lastValidated），currentTime 为当前时间戳，interval 为重新验证间隔。
// 若 key 尚未验证（时间戳为 0）或距上次验证时间已超过间隔，则视为过时。
// 返回所有过时 key 的列表。
func pcrFindStaleKeys(entries map[string]int64, currentTime int64, interval int) []string {
	stale := make([]string, 0)
	for key, lastValidated := range entries {
		if lastValidated == 0 || currentTime-lastValidated >= int64(interval) {
			stale = append(stale, key)
		}
	}
	return stale
}

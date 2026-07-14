package agent

import "sync"

// ── OPT-152: CacheCoherenceValidator (缓存一致性验证器) ──
// 验证并发操作的缓存一致性。通过版本号机制跟踪每个缓存键的版本，
// 在并发环境下检测版本不匹配（即缓存一致性违规），为缓存层提供
// 一致性保障与可观测性。
//
// 原理：在多协程并发访问缓存的场景中，可能出现「读-修改-写」竞态。
// 通过为每个键维护单调递增的版本号，在 Validate 时比对预期版本与
// 当前版本：若不匹配则说明该键在期间被其他协程修改过，即发生
// 一致性违规。每次 Validate 递增 checks 计数，违规时递增 violations。
//
// 效果：提供缓存一致性违规的检测与统计能力，帮助定位并发问题，
// 通过 GetViolationRate 可监控缓存一致性健康度。

// CacheCoherenceValidator 缓存一致性验证器
type CacheCoherenceValidator struct {
	mu         sync.RWMutex
	entries    map[string]int64 // key -> version
	violations int
	checks     int
}

// NewCacheCoherenceValidator 创建缓存一致性验证器。
func NewCacheCoherenceValidator() *CacheCoherenceValidator {
	return &CacheCoherenceValidator{
		entries: make(map[string]int64),
	}
}

// Validate 验证指定键的版本一致性。
// 若键不存在或版本不匹配，返回 false 并递增 violations 计数。
// 若版本匹配，返回 true。每次调用递增 checks 计数。
func (v *CacheCoherenceValidator) Validate(key string, version int64) bool {
	v.mu.Lock()
	defer v.mu.Unlock()

	v.checks++

	currentVersion, exists := v.entries[key]
	if !exists {
		v.violations++
		return false
	}

	if currentVersion != version {
		v.violations++
		return false
	}

	return true
}

// Update 更新指定键的版本号。若键不存在则创建。
func (v *CacheCoherenceValidator) Update(key string, version int64) {
	v.mu.Lock()
	defer v.mu.Unlock()

	v.entries[key] = version
}

// Remove 移除指定键的版本记录。
func (v *CacheCoherenceValidator) Remove(key string) {
	v.mu.Lock()
	defer v.mu.Unlock()

	delete(v.entries, key)
}

// GetViolationRate 返回缓存一致性违规率（violations / checks）。
// 若 checks 为 0 则返回 0。
func (v *CacheCoherenceValidator) GetViolationRate() float64 {
	v.mu.RLock()
	defer v.mu.RUnlock()

	return ccvComputeViolationRate(v.violations, v.checks)
}

// GetStats 返回验证器的统计信息，包括 trackedEntries、violations、checks、violationRate。
func (v *CacheCoherenceValidator) GetStats() map[string]interface{} {
	v.mu.RLock()
	defer v.mu.RUnlock()

	return map[string]interface{}{
		"trackedEntries": len(v.entries),
		"violations":     v.violations,
		"checks":         v.checks,
		"violationRate":  ccvComputeViolationRate(v.violations, v.checks),
	}
}

// Reset 重置验证器，清除所有版本记录与统计信息。
func (v *CacheCoherenceValidator) Reset() {
	v.mu.Lock()
	defer v.mu.Unlock()

	v.entries = make(map[string]int64)
	v.violations = 0
	v.checks = 0
}

// ---------------------------------------------------------------------------
// 辅助函数（以 ccv 为前缀，避免命名冲突）
// ---------------------------------------------------------------------------

// ccvComputeViolationRate 计算违规率: violations / checks。
// 若 checks 为 0，返回 0。
func ccvComputeViolationRate(violations, checks int) float64 {
	if checks == 0 {
		return 0
	}
	return float64(violations) / float64(checks)
}

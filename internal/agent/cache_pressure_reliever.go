package agent
import "sync"

// ── OPT-177: CachePressureReliever (缓存压力缓解器) ──
// 监控缓存条目数量，计算压力水平 (currentEntries / maxEntries)，
// 并在压力超过阈值时通过 Relieve 方法驱逐条目以降低压力。
// 累计缓解次数与缓解条目总数以便统计。

// CachePressureReliever 缓存压力缓解器，在缓存压力过高时自动缓解。
type CachePressureReliever struct {
	mu                sync.RWMutex
	maxEntries        int
	currentEntries    int
	pressureThreshold float64
	reliefCount       int
	totalRelieved     int
}

// NewCachePressureReliever 创建一个新的缓存压力缓解器。
// maxEntries 指定缓存最大条目数，pressureThreshold 指定触发缓解的压力阈值。
func NewCachePressureReliever(maxEntries int, pressureThreshold float64) *CachePressureReliever {
	return &CachePressureReliever{
		maxEntries:        maxEntries,
		pressureThreshold: pressureThreshold,
	}
}

// RecordInsert 记录一次缓存插入操作，递增当前条目数。
func (r *CachePressureReliever) RecordInsert() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.currentEntries++
}

// RecordEviction 记录一次缓存驱逐操作，递减当前条目数（不低于 0）。
func (r *CachePressureReliever) RecordEviction() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.currentEntries > 0 {
		r.currentEntries--
	}
}

// GetPressure 返回当前缓存压力值 (currentEntries / maxEntries)。
// 若 maxEntries <= 0 则返回 0。
func (r *CachePressureReliever) GetPressure() float64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return cprComputePressure(r.currentEntries, r.maxEntries)
}

// ShouldRelieve 判断当前压力是否超过阈值，超过则返回 true。
func (r *CachePressureReliever) ShouldRelieve() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	pressure := cprComputePressure(r.currentEntries, r.maxEntries)
	return pressure > r.pressureThreshold
}

// Relieve 缓解压力，驱逐至多 count 个条目。
// 返回实际缓解（驱逐）的条目数量，并递增 reliefCount 与 totalRelieved。
func (r *CachePressureReliever) Relieve(count int) int {
	r.mu.Lock()
	defer r.mu.Unlock()

	if count <= 0 || r.currentEntries <= 0 {
		return 0
	}

	relieved := count
	if relieved > r.currentEntries {
		relieved = r.currentEntries
	}
	r.currentEntries -= relieved
	r.reliefCount++
	r.totalRelieved += relieved
	return relieved
}

// GetStats 返回缓解器的统计信息，包括 maxEntries、currentEntries、
// pressureThreshold、reliefCount、totalRelieved 和当前 pressure。
func (r *CachePressureReliever) GetStats() map[string]interface{} {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return map[string]interface{}{
		"maxEntries":        r.maxEntries,
		"currentEntries":    r.currentEntries,
		"pressureThreshold": r.pressureThreshold,
		"reliefCount":       r.reliefCount,
		"totalRelieved":     r.totalRelieved,
		"pressure":          cprComputePressure(r.currentEntries, r.maxEntries),
	}
}

// Reset 重置缓解器的所有状态，保留 maxEntries 和 pressureThreshold 配置。
func (r *CachePressureReliever) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.currentEntries = 0
	r.reliefCount = 0
	r.totalRelieved = 0
}

// ── 辅助函数（cpr 前缀）──

// cprComputePressure 计算缓存压力值 (currentEntries / maxEntries)。
// 若 maxEntries <= 0 则返回 0。
func cprComputePressure(currentEntries int, maxEntries int) float64 {
	if maxEntries <= 0 {
		return 0
	}
	return float64(currentEntries) / float64(maxEntries)
}

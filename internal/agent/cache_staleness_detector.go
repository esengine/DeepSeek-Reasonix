package agent

import "sync"

// ── OPT-136: CacheStalenessDetector (缓存过期检测器) ──
// 检测缓存条目的陈旧程度。每条缓存条目在注册时记录其创建 turn，
// 后续可根据当前 turn 计算条目年龄，并判定为 fresh/stale/expired。
//
// 年龄区间:
//   - fresh:   age < threshold
//   - stale:   threshold <= age < maxAge
//   - expired: age >= maxAge
//
// 通过持续检测可统计陈旧率 (staleRate)，辅助缓存淘汰决策。

// CacheStalenessDetector 缓存过期检测器，检测缓存条目的陈旧程度。
type CacheStalenessDetector struct {
	mu            sync.RWMutex
	entries       map[string]int
	totalDetected int
	totalStale    int
	threshold     int
	maxAge        int
}

// NewCacheStalenessDetector 创建一个新的缓存过期检测器。
// threshold 为陈旧阈值，maxAge 为过期阈值（应满足 threshold <= maxAge）。
func NewCacheStalenessDetector(maxAge int, threshold int) *CacheStalenessDetector {
	return &CacheStalenessDetector{
		entries:   make(map[string]int),
		threshold: threshold,
		maxAge:    maxAge,
	}
}

// Register 注册缓存条目及其创建 turn。
// 若 key 已存在则覆盖其创建 turn。
func (d *CacheStalenessDetector) Register(key string, turn int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.entries[key] = turn
}

// CheckStaleness 检测指定缓存条目的陈旧程度。
// 返回 "fresh" (age < threshold)、"stale" (threshold <= age < maxAge)、
// "expired" (age >= maxAge) 或 "unknown" (条目未找到)。
// 每次成功检测（非 unknown）递增 totalDetected；"stale" 结果递增 totalStale。
func (d *CacheStalenessDetector) CheckStaleness(key string, currentTurn int) string {
	d.mu.Lock()
	defer d.mu.Unlock()

	turn, ok := d.entries[key]
	if !ok {
		return "unknown"
	}
	age := currentTurn - turn
	if age < 0 {
		age = 0
	}
	d.totalDetected++
	result := csdClassify(age, d.threshold, d.maxAge)
	if result == "stale" {
		d.totalStale++
	}
	return result
}

// GetStaleEntries 返回所有陈旧但未过期（stale）的条目键。
func (d *CacheStalenessDetector) GetStaleEntries(currentTurn int) []string {
	d.mu.RLock()
	defer d.mu.RUnlock()

	stale := make([]string, 0)
	for key, turn := range d.entries {
		age := currentTurn - turn
		if age < 0 {
			age = 0
		}
		if csdClassify(age, d.threshold, d.maxAge) == "stale" {
			stale = append(stale, key)
		}
	}
	return stale
}

// GetStats 返回检测器的统计信息。
// 包含 totalDetected、totalStale、staleRate、entryCount、maxAge、threshold。
func (d *CacheStalenessDetector) GetStats() map[string]interface{} {
	d.mu.RLock()
	defer d.mu.RUnlock()

	staleRate := 0.0
	if d.totalDetected > 0 {
		staleRate = float64(d.totalStale) / float64(d.totalDetected)
	}
	return map[string]interface{}{
		"totalDetected": d.totalDetected,
		"totalStale":    d.totalStale,
		"staleRate":     staleRate,
		"entryCount":    len(d.entries),
		"maxAge":        d.maxAge,
		"threshold":     d.threshold,
	}
}

// Reset 重置检测器的所有条目和统计信息。
func (d *CacheStalenessDetector) Reset() {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.entries = make(map[string]int)
	d.totalDetected = 0
	d.totalStale = 0
}

// ---------------------------------------------------------------------------
// 辅助函数 (csd 前缀)
// ---------------------------------------------------------------------------

// csdClassify 根据年龄、陈旧阈值与过期阈值判定陈旧类别。
func csdClassify(age, threshold, maxAge int) string {
	switch {
	case age < threshold:
		return "fresh"
	case age < maxAge:
		return "stale"
	default:
		return "expired"
	}
}

package agent
import "sync"

// OPT-240: PromptCacheEfficiencyMonitor — 提示缓存效率监控器
// PromptCacheEfficiencyMonitor monitors the overall efficiency of the prompt cache.
// It tracks cache hits, misses, tokens saved, and tokens processed to compute
// efficiency metrics that guide cache tuning decisions.
type PromptCacheEfficiencyMonitor struct {
	mu                   sync.RWMutex
	totalRequests        int   // 总请求数 total number of requests
	cacheHits            int   // 缓存命中数 number of cache hits
	cacheMisses          int   // 缓存未命中数 number of cache misses
	totalTokensSaved     int   // 累计节省的token总数 total tokens saved by cache
	totalTokensProcessed int   // 累计处理的token总数 total tokens processed
	monitoringDuration   int64 // 监控时长（秒） monitoring duration in seconds
}

// NewPromptCacheEfficiencyMonitor creates a new PromptCacheEfficiencyMonitor.
// NewPromptCacheEfficiencyMonitor 创建一个新的PromptCacheEfficiencyMonitor。
func NewPromptCacheEfficiencyMonitor() *PromptCacheEfficiencyMonitor {
	return &PromptCacheEfficiencyMonitor{
		totalRequests:        0,
		cacheHits:            0,
		cacheMisses:          0,
		totalTokensSaved:     0,
		totalTokensProcessed: 0,
		monitoringDuration:   0,
	}
}

// RecordRequest records a cache request with tokens saved, tokens processed, and hit status.
// RecordRequest 记录请求。
func (m *PromptCacheEfficiencyMonitor) RecordRequest(tokensSaved int, tokensProcessed int, hit bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.totalRequests++
	if hit {
		m.cacheHits++
	} else {
		m.cacheMisses++
	}
	m.totalTokensSaved += tokensSaved
	m.totalTokensProcessed += tokensProcessed
}

// SetMonitoringDuration sets the monitoring duration in seconds.
// SetMonitoringDuration 设置监控时长（秒）。
func (m *PromptCacheEfficiencyMonitor) SetMonitoringDuration(duration int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.monitoringDuration = duration
}

// GetEfficiency returns the cache efficiency ratio: totalTokensSaved / totalTokensProcessed.
// GetEfficiency 返回效率 = totalTokensSaved / totalTokensProcessed。
func (m *PromptCacheEfficiencyMonitor) GetEfficiency() float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.totalTokensProcessed == 0 {
		return 0.0
	}
	return float64(m.totalTokensSaved) / float64(m.totalTokensProcessed)
}

// GetHitRate returns the cache hit rate: cacheHits / totalRequests.
// GetHitRate 返回缓存命中率。
func (m *PromptCacheEfficiencyMonitor) GetHitRate() float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.totalRequests == 0 {
		return 0.0
	}
	return float64(m.cacheHits) / float64(m.totalRequests)
}

// GetTokensSavedRate returns the rate of tokens saved per minute.
// GetTokensSavedRate 返回每分钟节省的token数。
func (m *PromptCacheEfficiencyMonitor) GetTokensSavedRate() float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return pcemComputeRate(m.totalTokensSaved, m.monitoringDuration)
}

// GetStats returns statistics about the cache efficiency monitor.
// GetStats 返回监控器的统计信息。
func (m *PromptCacheEfficiencyMonitor) GetStats() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	efficiency := 0.0
	if m.totalTokensProcessed > 0 {
		efficiency = float64(m.totalTokensSaved) / float64(m.totalTokensProcessed)
	}
	hitRate := 0.0
	if m.totalRequests > 0 {
		hitRate = float64(m.cacheHits) / float64(m.totalRequests)
	}
	return map[string]interface{}{
		"totalRequests":        m.totalRequests,
		"cacheHits":            m.cacheHits,
		"cacheMisses":          m.cacheMisses,
		"totalTokensSaved":     m.totalTokensSaved,
		"totalTokensProcessed": m.totalTokensProcessed,
		"efficiency":           efficiency,
		"hitRate":              hitRate,
	}
}

// Reset resets the monitor to its initial state.
// Reset 重置监控器到初始状态。
func (m *PromptCacheEfficiencyMonitor) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.totalRequests = 0
	m.cacheHits = 0
	m.cacheMisses = 0
	m.totalTokensSaved = 0
	m.totalTokensProcessed = 0
	m.monitoringDuration = 0
}

// pcemComputeRate computes the tokens-saved-per-minute rate given total tokens saved and duration in seconds.
// pcemComputeRate 计算每分钟节省的token数。
func pcemComputeRate(totalTokensSaved int, monitoringDuration int64) float64 {
	if monitoringDuration <= 0 {
		return 0.0
	}
	minutes := float64(monitoringDuration) / 60.0
	if minutes == 0 {
		return 0.0
	}
	return float64(totalTokensSaved) / minutes
}

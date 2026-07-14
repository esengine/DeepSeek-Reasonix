package agent

import "sync"

// CachePressureMonitor (OPT-104) 缓存压力监控器。
// 监控缓存条目数量，计算压力水平，并在压力过高时触发驱逐建议。
type CachePressureMonitor struct {
	mu              sync.RWMutex
	maxEntries      int
	currentEntries  int
	pressureLevel   float64
	evictions       int
	pressureHistory []float64
	thresholdHigh   float64
	thresholdLow    float64
}

// NewCachePressureMonitor 创建一个新的 CachePressureMonitor 实例。
// maxEntries 指定缓存最大条目数，高阈值设为 0.85，低阈值设为 0.6。
func NewCachePressureMonitor(maxEntries int) *CachePressureMonitor {
	return &CachePressureMonitor{
		maxEntries:    maxEntries,
		thresholdHigh: 0.85,
		thresholdLow:  0.6,
	}
}

// RecordInsert 记录一次缓存插入操作，并更新压力水平。
func (m *CachePressureMonitor) RecordInsert() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.currentEntries++
	m.pressureLevel = m.calculatePressureLocked()
	m.pressureHistory = append(m.pressureHistory, m.pressureLevel)
}

// RecordEviction 记录一次缓存驱逐操作，并更新压力水平。
func (m *CachePressureMonitor) RecordEviction() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.currentEntries > 0 {
		m.currentEntries--
	}
	m.evictions++
	m.pressureLevel = m.calculatePressureLocked()
	m.pressureHistory = append(m.pressureHistory, m.pressureLevel)
}

// CalculatePressure 计算当前缓存压力水平。
// pressureLevel = currentEntries / maxEntries
func (m *CachePressureMonitor) CalculatePressure() float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pressureLevel = m.calculatePressureLocked()
	return m.pressureLevel
}

// calculatePressureLocked 在已加锁的情况下计算压力水平。
func (m *CachePressureMonitor) calculatePressureLocked() float64 {
	if m.maxEntries <= 0 {
		return 0
	}
	return float64(m.currentEntries) / float64(m.maxEntries)
}

// ShouldEvict 判断是否应该执行缓存驱逐。
// 当压力水平超过高阈值时返回 true。
func (m *CachePressureMonitor) ShouldEvict() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.pressureLevel > m.thresholdHigh
}

// GetPressureLevel 返回当前压力水平的文本标签。
// 返回 "low"、"medium"、"high" 或 "critical"。
func (m *CachePressureMonitor) GetPressureLevel() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.pressureLevelLabelLocked()
}

// pressureLevelLabelLocked 在已加锁的情况下返回压力水平标签。
func (m *CachePressureMonitor) pressureLevelLabelLocked() string {
	switch {
	case m.pressureLevel >= 0.95:
		return "critical"
	case m.pressureLevel >= m.thresholdHigh:
		return "high"
	case m.pressureLevel >= m.thresholdLow:
		return "medium"
	default:
		return "low"
	}
}

// GetStats 返回监控器的统计信息。
func (m *CachePressureMonitor) GetStats() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return map[string]interface{}{
		"maxEntries":          m.maxEntries,
		"currentEntries":      m.currentEntries,
		"pressureLevel":       m.pressureLevel,
		"evictions":           m.evictions,
		"pressureLevel_label": m.pressureLevelLabelLocked(),
	}
}

// Reset 重置监控器的所有状态，但保留 maxEntries 和阈值配置。
func (m *CachePressureMonitor) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.currentEntries = 0
	m.pressureLevel = 0
	m.evictions = 0
	m.pressureHistory = nil
}

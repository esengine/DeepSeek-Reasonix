package agent
import "sync"

// ── OPT-163: ContextWindowMonitor (上下文窗口监控器 / Context Window Monitor) ──
// 实时监控上下文窗口的使用情况，记录使用采样、峰值与平均用量，
// 便于在接近窗口上限时触发裁剪或压缩策略。
//
// 原理：持续采样 usedTokens，维护最近 maxSamples 条记录，计算利用率、
// 峰值与平均使用量，为上层决策提供量化依据。
//
// 效果：避免上下文溢出，及时触发裁剪，降低因超限导致的截断风险。

// WindowSample 窗口使用采样，Timestamp 为逻辑序号（按采样顺序递增）。
type WindowSample struct {
	Timestamp  int64
	UsedTokens int
}

// ContextWindowMonitor 上下文窗口监控器，记录窗口使用采样与统计。
type ContextWindowMonitor struct {
	mu         sync.RWMutex
	windowSize int
	usedTokens int
	peakUsage  int
	samples    []WindowSample
	maxSamples int
}

// NewContextWindowMonitor 创建一个新的 ContextWindowMonitor。
// windowSize 指定上下文窗口大小（token 数），maxSamples 固定为 100。
// 若 windowSize <=0 则默认 128000。
func NewContextWindowMonitor(windowSize int) *ContextWindowMonitor {
	if windowSize <= 0 {
		windowSize = 128000
	}
	return &ContextWindowMonitor{
		windowSize: windowSize,
		maxSamples: 100,
	}
}

// Record 记录一次窗口使用采样，更新当前使用量、峰值，并追加采样。
// 超过 maxSamples 时丢弃最早的采样，保留最近的记录。
func (m *ContextWindowMonitor) Record(usedTokens int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.usedTokens = usedTokens
	if usedTokens > m.peakUsage {
		m.peakUsage = usedTokens
	}
	var ts int64
	if len(m.samples) > 0 {
		ts = m.samples[len(m.samples)-1].Timestamp + 1
	} else {
		ts = 0
	}
	m.samples = append(m.samples, WindowSample{Timestamp: ts, UsedTokens: usedTokens})
	if len(m.samples) > m.maxSamples {
		m.samples = m.samples[len(m.samples)-m.maxSamples:]
	}
}

// GetUtilization 返回当前窗口利用率：usedTokens / windowSize。
func (m *ContextWindowMonitor) GetUtilization() float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.windowSize <= 0 {
		return 0
	}
	return float64(m.usedTokens) / float64(m.windowSize)
}

// GetPeakUsage 返回历史峰值使用量。
func (m *ContextWindowMonitor) GetPeakUsage() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.peakUsage
}

// GetAvgUsage 返回最近采样的平均使用量。
func (m *ContextWindowMonitor) GetAvgUsage() float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return cwmComputeAvg(m.samples)
}

// GetStats 返回监控器的统计信息，包括 windowSize、usedTokens、peakUsage、
// utilization、avgUsage 和 sampleCount。
func (m *ContextWindowMonitor) GetStats() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()
	util := 0.0
	if m.windowSize > 0 {
		util = float64(m.usedTokens) / float64(m.windowSize)
	}
	return map[string]interface{}{
		"windowSize":  m.windowSize,
		"usedTokens":  m.usedTokens,
		"peakUsage":   m.peakUsage,
		"utilization": util,
		"avgUsage":    cwmComputeAvg(m.samples),
		"sampleCount": len(m.samples),
	}
}

// Reset 重置监控器的所有状态，清空采样与统计（保留 windowSize 与 maxSamples 配置）。
func (m *ContextWindowMonitor) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.usedTokens = 0
	m.peakUsage = 0
	m.samples = nil
}

// cwmComputeAvg 计算采样列表的平均使用量，无采样时返回 0。
func cwmComputeAvg(samples []WindowSample) float64 {
	if len(samples) == 0 {
		return 0
	}
	total := 0
	for _, s := range samples {
		total += s.UsedTokens
	}
	return float64(total) / float64(len(samples))
}

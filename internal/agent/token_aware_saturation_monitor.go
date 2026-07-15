package agent
import "sync"

// ── OPT-244: TokenAwareSaturationMonitor (Token感知饱和度监控器 / Token-Aware Saturation Monitor) ──
// 监控系统 token 使用的饱和程度，提供当前饱和度、历史平均饱和度与告警。
// 饱和度 = 当前使用量 / 容量，范围 0~1。
// 当饱和度超过 0.8 时记为告警并累计告警次数。
// 历史记录按 maxHistorySize 滑动窗口保留。

// TokenAwareSaturationMonitor Token感知饱和度监控器
type TokenAwareSaturationMonitor struct {
	mu                sync.RWMutex
	capacity          int       // 系统容量
	currentUsage      int       // 当前 token 使用量
	saturationHistory []float64 // 饱和度历史记录
	maxHistorySize    int       // 最大历史记录条数
	alertCount        int       // 告警次数
}

// NewTokenAwareSaturationMonitor 创建一个新的 Token 感知饱和度监控器实例。
// capacity 指定系统容量，maxHistorySize 指定饱和度历史记录最大条数。
func NewTokenAwareSaturationMonitor(capacity int, maxHistorySize int) *TokenAwareSaturationMonitor {
	return &TokenAwareSaturationMonitor{
		capacity:          capacity,
		maxHistorySize:    maxHistorySize,
		saturationHistory: make([]float64, 0),
	}
}

// Record 记录一次使用量，返回当前饱和度(0~1)。
// 饱和度超过 0.8 时累加告警次数，并将饱和度写入滑动窗口历史。
func (t *TokenAwareSaturationMonitor) Record(usage int) float64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.currentUsage = usage
	saturation := 0.0
	if t.capacity > 0 {
		saturation = float64(usage) / float64(t.capacity)
		if saturation > 1.0 {
			saturation = 1.0
		}
	}
	if saturation > 0.8 {
		t.alertCount++
	}
	if t.maxHistorySize > 0 && len(t.saturationHistory) >= t.maxHistorySize {
		t.saturationHistory = t.saturationHistory[1:]
	}
	t.saturationHistory = append(t.saturationHistory, saturation)
	return saturation
}

// GetSaturation 获取当前饱和度(0~1)。
// 容量非正时返回 0。
func (t *TokenAwareSaturationMonitor) GetSaturation() float64 {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if t.capacity <= 0 {
		return 0
	}
	saturation := float64(t.currentUsage) / float64(t.capacity)
	if saturation > 1.0 {
		saturation = 1.0
	}
	return saturation
}

// GetAvgSaturation 获取历史平均饱和度。
// 历史为空时返回 0。
func (t *TokenAwareSaturationMonitor) GetAvgSaturation() float64 {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return tasmComputeAvg(t.saturationHistory)
}

// IsSaturated 判断当前饱和度是否超过 0.8。
func (t *TokenAwareSaturationMonitor) IsSaturated() bool {
	return t.GetSaturation() > 0.8
}

// GetStats 获取统计信息。
// 返回 capacity、currentUsage、currentSaturation、avgSaturation、alertCount。
func (t *TokenAwareSaturationMonitor) GetStats() map[string]interface{} {
	t.mu.RLock()
	defer t.mu.RUnlock()
	saturation := 0.0
	if t.capacity > 0 {
		saturation = float64(t.currentUsage) / float64(t.capacity)
		if saturation > 1.0 {
			saturation = 1.0
		}
	}
	return map[string]interface{}{
		"capacity":          t.capacity,
		"currentUsage":      t.currentUsage,
		"currentSaturation": saturation,
		"avgSaturation":     tasmComputeAvg(t.saturationHistory),
		"alertCount":        t.alertCount,
	}
}

// Reset 重置使用量、历史记录与告警次数。
// 保留容量与最大历史记录条数配置。
func (t *TokenAwareSaturationMonitor) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.currentUsage = 0
	t.saturationHistory = make([]float64, 0)
	t.alertCount = 0
}

// tasmComputeAvg 辅助函数，计算饱和度历史记录的平均值。
// 历史为空时返回 0。
func tasmComputeAvg(history []float64) float64 {
	if len(history) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range history {
		sum += v
	}
	return sum / float64(len(history))
}

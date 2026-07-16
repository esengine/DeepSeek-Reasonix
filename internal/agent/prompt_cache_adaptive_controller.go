package agent

import "sync"

// OPT-250: PromptCacheAdaptiveController / 提示缓存自适应控制器
// 根据历史命中率自适应调整缓存策略。
type PromptCacheAdaptiveController struct {
	mu                 sync.RWMutex
	strategy           string
	adjustments        int
	lastHitRate        float64
	performanceHistory []float64
	maxHistorySize     int
	adaptiveMode       bool
}

// NewPromptCacheAdaptiveController 创建一个新的提示缓存自适应控制器。
// maxHistorySize 为历史记录上限，非正值时默认为100。
func NewPromptCacheAdaptiveController(maxHistorySize int) *PromptCacheAdaptiveController {
	if maxHistorySize <= 0 {
		maxHistorySize = 100
	}
	return &PromptCacheAdaptiveController{
		strategy:           "default",
		maxHistorySize:     maxHistorySize,
		performanceHistory: make([]float64, 0),
		adaptiveMode:       true,
	}
}

// RecordPerformance 记录一次性能指标（命中率），并维护历史记录不超过上限。
func (p *PromptCacheAdaptiveController) RecordPerformance(hitRate float64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.lastHitRate = hitRate
	p.performanceHistory = append(p.performanceHistory, hitRate)
	if len(p.performanceHistory) > p.maxHistorySize {
		p.performanceHistory = p.performanceHistory[len(p.performanceHistory)-p.maxHistorySize:]
	}
}

// Adapt 根据历史性能自适应调整策略，返回新策略。
// 若自适应模式关闭则直接返回当前策略。
func (p *PromptCacheAdaptiveController) Adapt() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.adaptiveMode {
		return p.strategy
	}
	avg := pcacComputeAvg(p.performanceHistory)
	newStrategy := pcacSelectStrategy(avg)
	if newStrategy != p.strategy {
		p.strategy = newStrategy
		p.adjustments++
	}
	return p.strategy
}

// GetStrategy 获取当前缓存策略。
func (p *PromptCacheAdaptiveController) GetStrategy() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.strategy
}

// IsAdaptive 返回是否处于自适应模式。
func (p *PromptCacheAdaptiveController) IsAdaptive() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.adaptiveMode
}

// GetStats 获取统计信息，包含 strategy、adjustments、lastHitRate、adaptiveMode、historySize、avgHitRate。
func (p *PromptCacheAdaptiveController) GetStats() map[string]interface{} {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return map[string]interface{}{
		"strategy":     p.strategy,
		"adjustments":  p.adjustments,
		"lastHitRate":  p.lastHitRate,
		"adaptiveMode": p.adaptiveMode,
		"historySize":  len(p.performanceHistory),
		"avgHitRate":   pcacComputeAvg(p.performanceHistory),
	}
}

// Reset 重置所有状态，策略恢复为 default，清空历史记录与计数器。
func (p *PromptCacheAdaptiveController) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.strategy = "default"
	p.adjustments = 0
	p.lastHitRate = 0
	p.performanceHistory = make([]float64, 0)
	p.adaptiveMode = true
}

// pcacComputeAvg 计算性能历史的平均命中率（辅助函数）。
func pcacComputeAvg(history []float64) float64 {
	if len(history) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range history {
		sum += v
	}
	return sum / float64(len(history))
}

// pcacSelectStrategy 根据平均命中率选择缓存策略（辅助函数）。
// 高命中率采用激进策略，低命中率采用保守策略。
func pcacSelectStrategy(avgHitRate float64) string {
	switch {
	case avgHitRate >= 0.8:
		return "aggressive"
	case avgHitRate >= 0.5:
		return "balanced"
	case avgHitRate >= 0.2:
		return "conservative"
	default:
		return "minimal"
	}
}

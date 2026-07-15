package agent
import "sync"

// ── OPT-234: TokenAwareLeakDetector (Token感知泄漏检测器 / Token-Aware Leak Detector) ──
// 检测 token 使用中的泄漏模式。为每个来源设置基线使用量，
// 当后续使用量超过基线时判定为泄漏，累计泄漏次数和泄漏 token 数量。
// 可查询存在泄漏的来源列表，帮助定位 token 浪费。

// TokenAwareLeakDetector Token感知泄漏检测器
type TokenAwareLeakDetector struct {
	mu                sync.RWMutex
	baselines         map[string]int // source -> 基线使用量
	currentUsage      map[string]int // source -> 当前使用量
	leaksDetected     int            // 泄漏检测次数
	totalLeakedTokens int            // 累计泄漏 token 数
}

// NewTokenAwareLeakDetector 创建一个新的 Token 感知泄漏检测器。
func NewTokenAwareLeakDetector() *TokenAwareLeakDetector {
	return &TokenAwareLeakDetector{
		baselines:    make(map[string]int),
		currentUsage: make(map[string]int),
	}
}

// SetBaseline 设置指定来源的基线 token 使用量。
// 基线是该来源正常情况下的预期使用量，后续 CheckUsage 与之比较。
func (d *TokenAwareLeakDetector) SetBaseline(source string, tokens int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.baselines[source] = tokens
}

// CheckUsage 检查指定来源的当前使用量与基线的差异。
// 若当前使用量超过基线，判定为泄漏，返回泄漏量（正数）。
// 若未设置基线或未超过基线，返回 0。
func (d *TokenAwareLeakDetector) CheckUsage(source string, currentTokens int) int {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.currentUsage[source] = currentTokens

	baseline, exists := d.baselines[source]
	if !exists {
		return 0
	}

	// 泄漏量 = 当前使用量 - 基线（仅当超过基线时算泄漏）
	leak := currentTokens - baseline
	if leak > 0 {
		d.leaksDetected++
		d.totalLeakedTokens += leak
		return leak
	}

	return 0
}

// GetLeakSources 获取存在泄漏的来源列表。
// 泄漏条件：当前使用量超过基线。
func (d *TokenAwareLeakDetector) GetLeakSources() []string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return taldFindLeakSources(d.baselines, d.currentUsage)
}

// GetStats 返回泄漏检测器的统计信息。
// 包含 trackedSources、leaksDetected、totalLeakedTokens、leakSourceCount。
func (d *TokenAwareLeakDetector) GetStats() map[string]interface{} {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return map[string]interface{}{
		"trackedSources":     len(d.baselines),
		"leaksDetected":      d.leaksDetected,
		"totalLeakedTokens":  d.totalLeakedTokens,
		"leakSourceCount":    len(taldFindLeakSources(d.baselines, d.currentUsage)),
	}
}

// Reset 重置泄漏检测器的所有状态。
func (d *TokenAwareLeakDetector) Reset() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.baselines = make(map[string]int)
	d.currentUsage = make(map[string]int)
	d.leaksDetected = 0
	d.totalLeakedTokens = 0
}

// taldFindLeakSources 查找存在泄漏的来源列表（辅助函数）。
// 泄漏条件：当前使用量 > 基线使用量。
func taldFindLeakSources(baselines map[string]int, currentUsage map[string]int) []string {
	sources := make([]string, 0)
	for source, baseline := range baselines {
		current, exists := currentUsage[source]
		if exists && current > baseline {
			sources = append(sources, source)
		}
	}
	return sources
}

package agent

import "sync"

// ── OPT-255: PromptCacheHitPredictorV2 (提示缓存命中预测器V2 / Prompt Cache Hit Predictor V2) ──
// 基于历史命中模式预测提示缓存是否命中。
// 维护 pattern→hitCount 的映射，当历史命中次数达到置信阈值时预测命中。
//
// 原理：RecordResult 记录实际命中结果并更新 pattern 的命中次数；
// Predict 基于历史命中次数与置信阈值判断是否预测命中。
// 准确率 = correctPredictions / totalPredictions。
//
// 效果：预测缓存命中以指导预热策略，统计预测准确率，
// 为缓存管理提供数据支撑。

// PromptCacheHitPredictorV2 提示缓存命中预测器V2
type PromptCacheHitPredictorV2 struct {
	mu                  sync.RWMutex
	patterns            map[string]int // pattern → hitCount
	totalPredictions    int
	correctPredictions  int
	confidenceThreshold float64
}

// NewPromptCacheHitPredictorV2 创建提示缓存命中预测器V2。
// confidenceThreshold 指定预测命中的置信阈值（最小历史命中次数），
// 若 <= 0 则默认 0.5。
func NewPromptCacheHitPredictorV2(confidenceThreshold float64) *PromptCacheHitPredictorV2 {
	if confidenceThreshold <= 0 {
		confidenceThreshold = 0.5
	}
	return &PromptCacheHitPredictorV2{
		patterns:            make(map[string]int),
		confidenceThreshold: confidenceThreshold,
	}
}

// Predict 预测是否命中（基于历史模式）。
// 递增 totalPredictions。若该 key 的历史命中次数 >= confidenceThreshold 则预测命中。
func (p *PromptCacheHitPredictorV2) Predict(key string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.totalPredictions++
	return float64(p.patterns[key]) >= p.confidenceThreshold
}

// RecordResult 记录实际结果并更新 pattern 命中次数与预测准确率。
// 基于当前历史模式的预测与实际结果一致时递增 correctPredictions。
// 若 hit 为 true 则递增该 key 的 hitCount。
func (p *PromptCacheHitPredictorV2) RecordResult(key string, hit bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	predicted := float64(p.patterns[key]) >= p.confidenceThreshold
	if predicted == hit {
		p.correctPredictions++
	}
	if hit {
		p.patterns[key]++
	}
}

// GetAccuracy 获取预测准确率 = correctPredictions / totalPredictions。
// 若 totalPredictions == 0 则返回 0。
func (p *PromptCacheHitPredictorV2) GetAccuracy() float64 {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return pchpv2ComputeAccuracy(p.correctPredictions, p.totalPredictions)
}

// GetStats 返回预测器的统计信息。
// 包含 patternCount、totalPredictions、correctPredictions、accuracy 和 confidenceThreshold。
func (p *PromptCacheHitPredictorV2) GetStats() map[string]interface{} {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return map[string]interface{}{
		"patternCount":        len(p.patterns),
		"totalPredictions":    p.totalPredictions,
		"correctPredictions":  p.correctPredictions,
		"accuracy":            pchpv2ComputeAccuracy(p.correctPredictions, p.totalPredictions),
		"confidenceThreshold": p.confidenceThreshold,
	}
}

// Reset 重置预测器的模式和计数（保留 confidenceThreshold 配置）。
func (p *PromptCacheHitPredictorV2) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.patterns = make(map[string]int)
	p.totalPredictions = 0
	p.correctPredictions = 0
}

// pchpv2ComputeAccuracy 计算准确率 = correct / total。
// 若 total <= 0 则返回 0。
func pchpv2ComputeAccuracy(correct int, total int) float64 {
	if total <= 0 {
		return 0
	}
	return float64(correct) / float64(total)
}

package agent

import "sync"

// ── OPT-147: CachePopulationPredictor (缓存填充预测器) ──
// 预测缓存将被填充的概率。基于历史填充模式为每个 key 维护填充率，
// 预测时返回该 key 的历史填充率；若无历史记录则返回整体填充率。
// 通过 RecordFill 记录实际结果并更新预测准确性统计。

// CachePopulationPredictor 缓存填充预测器，预测缓存将被填充的概率。
type CachePopulationPredictor struct {
	mu                  sync.RWMutex
	totalPredictions    int
	accuratePredictions int
	fillPatterns        map[string]float64
	historicalFills     []bool
	maxHistorySize      int
	// 内部辅助字段
	fillCounts      map[string]int     // 每个 key 的记录次数
	lastPredictions map[string]float64 // 每个 key 最近一次的预测值
}

// NewCachePopulationPredictor 创建一个新的缓存填充预测器。
// maxHistorySize 默认为 50。
func NewCachePopulationPredictor() *CachePopulationPredictor {
	return &CachePopulationPredictor{
		fillPatterns:    make(map[string]float64),
		historicalFills: []bool{},
		maxHistorySize:  50,
		fillCounts:      make(map[string]int),
		lastPredictions: make(map[string]float64),
	}
}

// PredictFill 预测给定 key 将被填充的概率。
// 若该 key 有历史填充模式，返回基于模式的概率；否则返回整体历史填充率。
func (p *CachePopulationPredictor) PredictFill(key string) float64 {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.totalPredictions++

	var prediction float64
	if rate, ok := p.fillPatterns[key]; ok {
		prediction = rate
	} else {
		prediction = cppComputeFillRate(p.historicalFills)
	}
	p.lastPredictions[key] = prediction
	return prediction
}

// RecordFill 记录某个 key 的实际填充结果，并更新预测准确性和填充模式。
func (p *CachePopulationPredictor) RecordFill(key string, filled bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// 检查预测准确性
	if pred, ok := p.lastPredictions[key]; ok {
		if cppIsAccurate(pred, filled) {
			p.accuratePredictions++
		}
		delete(p.lastPredictions, key)
	}

	// 更新历史记录
	p.historicalFills = append(p.historicalFills, filled)
	if len(p.historicalFills) > p.maxHistorySize {
		p.historicalFills = p.historicalFills[len(p.historicalFills)-p.maxHistorySize:]
	}

	// 更新填充模式（运行平均值）
	count := p.fillCounts[key] + 1
	oldRate := p.fillPatterns[key]
	newRate := (oldRate*float64(count-1) + cppBoolToFloat(filled)) / float64(count)
	p.fillPatterns[key] = newRate
	p.fillCounts[key] = count
}

// GetFillRate 返回历史填充率。
func (p *CachePopulationPredictor) GetFillRate() float64 {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return cppComputeFillRate(p.historicalFills)
}

// GetStats 返回预测器的统计信息，包括 totalPredictions、accuratePredictions、
// accuracy、fillRate 和 patternCount。
func (p *CachePopulationPredictor) GetStats() map[string]interface{} {
	p.mu.RLock()
	defer p.mu.RUnlock()

	accuracy := 0.0
	if p.totalPredictions > 0 {
		accuracy = float64(p.accuratePredictions) / float64(p.totalPredictions)
	}

	return map[string]interface{}{
		"totalPredictions":    p.totalPredictions,
		"accuratePredictions": p.accuratePredictions,
		"accuracy":            accuracy,
		"fillRate":            cppComputeFillRate(p.historicalFills),
		"patternCount":        len(p.fillPatterns),
	}
}

// Reset 重置预测器的所有状态。
func (p *CachePopulationPredictor) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.totalPredictions = 0
	p.accuratePredictions = 0
	p.fillPatterns = make(map[string]float64)
	p.historicalFills = []bool{}
	p.fillCounts = make(map[string]int)
	p.lastPredictions = make(map[string]float64)
}

// cppBoolToFloat 将布尔值转换为浮点数（true→1.0, false→0.0）。
func cppBoolToFloat(b bool) float64 {
	if b {
		return 1.0
	}
	return 0.0
}

// cppComputeFillRate 计算历史填充率（已填充数量 / 总数量）。
func cppComputeFillRate(history []bool) float64 {
	if len(history) == 0 {
		return 0.0
	}
	filled := 0
	for _, b := range history {
		if b {
			filled++
		}
	}
	return float64(filled) / float64(len(history))
}

// cppIsAccurate 判断预测是否准确。
// 预测概率 >= 0.5 且实际填充为 true，或预测概率 < 0.5 且实际填充为 false，视为准确。
func cppIsAccurate(prediction float64, actual bool) bool {
	if prediction >= 0.5 {
		return actual
	}
	return !actual
}

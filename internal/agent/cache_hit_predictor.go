package agent

import "sync"

// ── OPT-91: CacheHitPredictor (缓存命中预测器) ──
// 预测下一次请求是否能够命中 prompt 缓存，从而提前决定是否需要
// 重建上下文或可以复用缓存前缀。
//
// 原理：prompt 缓存命中取决于前缀是否稳定。当系统提示词/工具
// schema 的哈希发生变化，或工具集合发生增删时，缓存前缀必然
// 失效。CacheHitPredictor 基于这些信号做出预测，并记录实际命中
// 情况，用以校准预测准确率与平均命中率。
//
// 效果：通过提前预测缓存命中/未命中，可以避免不必要的上下文重建
// 与缓存探测开销，在命中率较高时直接复用稳定前缀。

// CacheHitPredictorStats 缓存命中预测器的统计信息
type CacheHitPredictorStats struct {
	TotalPredictions   int     // 预测总次数
	CorrectPredictions int     // 预测正确次数
	Accuracy           float64 // 预测准确率（正确次数/总次数）
	AvgHitRate         float64 // 平均实际命中率
}

// CacheHitPredictor 预测下一次请求是否命中缓存。
// 基于前缀哈希稳定性与工具变更信号进行预测，并跟踪实际命中情况。
type CacheHitPredictor struct {
	mu                 sync.RWMutex
	totalPredictions   int
	correctPredictions int
	hitHistory         []bool
	avgHitRate         float64

	// lastPrediction 记录最近一次 PredictHit 的预测结果，
	// 供 RecordActualHit 判断预测是否正确。
	lastPrediction bool
}

// NewCacheHitPredictor 创建缓存命中预测器
func NewCacheHitPredictor() *CacheHitPredictor {
	return &CacheHitPredictor{
		hitHistory: make([]bool, 0),
	}
}

// PredictHit 预测给定前缀哈希是否会导致缓存命中。
//
// 预测规则：若前缀发生变化（prefixHash != lastHash）或工具集合
// 发生变更（toolsChanged），则预测为未命中（false）；否则预测命中。
// 每次调用递增预测总次数。
//
// 返回预测结果（true 表示预测命中）。
func (p *CacheHitPredictor) PredictHit(prefixHash string, lastHash string, toolsChanged bool) bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.totalPredictions++
	hit := !toolsChanged && prefixHash == lastHash
	p.lastPrediction = hit
	return hit
}

// RecordActualHit 记录实际命中情况，并据此校准预测准确率与平均命中率。
//
// 将实际命中结果与最近一次预测对比：若一致则递增正确预测计数。
// 同时将实际命中结果追加到历史记录并更新平均命中率。
func (p *CacheHitPredictor) RecordActualHit(wasHit bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if wasHit == p.lastPrediction {
		p.correctPredictions++
	}

	p.hitHistory = append(p.hitHistory, wasHit)

	hits := 0
	for _, h := range p.hitHistory {
		if h {
			hits++
		}
	}
	if len(p.hitHistory) > 0 {
		p.avgHitRate = float64(hits) / float64(len(p.hitHistory))
	}
}

// GetHitRate 返回从历史记录计算的平均命中率
func (p *CacheHitPredictor) GetHitRate() float64 {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.avgHitRate
}

// GetStats 返回预测器的统计信息
func (p *CacheHitPredictor) GetStats() CacheHitPredictorStats {
	p.mu.RLock()
	defer p.mu.RUnlock()

	var accuracy float64
	if p.totalPredictions > 0 {
		accuracy = float64(p.correctPredictions) / float64(p.totalPredictions)
	}

	return CacheHitPredictorStats{
		TotalPredictions:   p.totalPredictions,
		CorrectPredictions: p.correctPredictions,
		Accuracy:           accuracy,
		AvgHitRate:         p.avgHitRate,
	}
}

// Reset 重置预测器，清除所有统计与历史记录
func (p *CacheHitPredictor) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.totalPredictions = 0
	p.correctPredictions = 0
	p.hitHistory = nil
	p.avgHitRate = 0
	p.lastPrediction = false
}

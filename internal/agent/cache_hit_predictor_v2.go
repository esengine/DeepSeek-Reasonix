package agent
import "sync"

// ── OPT-182: CacheHitPredictorV2 (缓存命中预测器 V2 / Cache-Hit Predictor V2) ──
// 基于历史命中模式按 key 预测缓存命中概率。与 OPT-91 CacheHitPredictor
// （基于前缀哈希与工具变更信号）不同，V2 维护每个 key 的命中历史序列，
// 以历史命中率作为预测概率，并跟踪预测准确率。
//
// 注：因包内已存在 OPT-91 的 CacheHitPredictor 类型，本模块以 V2 后缀
// 命名以避免命名冲突，二者可共存。

// CacheHitPredictorV2 缓存命中预测器 V2。
type CacheHitPredictorV2 struct {
	mu                 sync.RWMutex
	history            map[string][]bool // key → 命中历史（true=命中）
	predictions        int               // 累计预测次数
	correctPredictions int               // 累计预测正确次数
}

// NewCacheHitPredictorV2 创建缓存命中预测器 V2。
func NewCacheHitPredictorV2() *CacheHitPredictorV2 {
	return &CacheHitPredictorV2{
		history: make(map[string][]bool),
	}
}

// RecordHit 记录一次缓存的命中或未命中。
// key 为缓存键，hit 为 true 表示命中。
func (p *CacheHitPredictorV2) RecordHit(key string, hit bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.history[key] = append(p.history[key], hit)
}

// Predict 基于历史命中率预测指定 key 的命中概率。
// 返回值域 [0,1]；若无历史记录则返回 0。
// 每次调用递增预测计数，并据此校准预测准确率。
func (p *CacheHitPredictorV2) Predict(key string) float64 {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.predictions++
	hist := p.history[key]
	prob := chpComputeHitRate(hist)

	// 校准准确率：将预测概率四舍五入为布尔预测，
	// 与最近一次实际命中结果对比。
	if len(hist) > 0 {
		predictedHit := prob >= 0.5
		actualHit := hist[len(hist)-1]
		if predictedHit == actualHit {
			p.correctPredictions++
		}
	}
	return prob
}

// GetAccuracy 返回预测准确率（正确预测数 / 总预测数）。
// 若无预测记录则返回 0。
func (p *CacheHitPredictorV2) GetAccuracy() float64 {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return chpComputeAccuracy(p.predictions, p.correctPredictions)
}

// GetStats 返回预测器统计信息，包括 trackedKeys、predictions、
// correctPredictions 与 accuracy。
func (p *CacheHitPredictorV2) GetStats() map[string]interface{} {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return map[string]interface{}{
		"trackedKeys":        len(p.history),
		"predictions":        p.predictions,
		"correctPredictions": p.correctPredictions,
		"accuracy":           chpComputeAccuracy(p.predictions, p.correctPredictions),
	}
}

// Reset 重置预测器，清除所有历史与计数。
func (p *CacheHitPredictorV2) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.history = make(map[string][]bool)
	p.predictions = 0
	p.correctPredictions = 0
}

// chpComputeHitRate 根据命中历史序列计算命中率。
// 返回命中次数 / 总次数；若序列为空则返回 0。
func chpComputeHitRate(history []bool) float64 {
	if len(history) == 0 {
		return 0
	}
	hits := 0
	for _, h := range history {
		if h {
			hits++
		}
	}
	return float64(hits) / float64(len(history))
}

// chpComputeAccuracy 根据预测次数与正确次数计算准确率。
// 若预测次数为 0 则返回 0。
func chpComputeAccuracy(predictions int, correctPredictions int) float64 {
	if predictions == 0 {
		return 0
	}
	return float64(correctPredictions) / float64(predictions)
}

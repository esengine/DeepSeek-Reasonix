package agent

import "sync"

// ── OPT-112: TokenBudgetPredictor (Token 预算预测器) ──
// 基于历史 token 使用数据预测未来消耗量，并跟踪预测准确率。
//
// 原理：记录最近 N 次 token 使用量，取平均值作为下一次预测值。
// 当实际值与预测值误差 < 20% 时计为预测准确，用以校准预测模型。
//
// 效果：通过提前预测 token 消耗，可以更好地分配预算和调度资源。

// TokenBudgetPredictor Token 预算预测器，基于历史数据预测未来消耗。
type TokenBudgetPredictor struct {
	mu                  sync.RWMutex
	history             []int
	totalPredictions    int
	accuratePredictions int
	lastPrediction      int
	maxHistorySize      int
}

// NewTokenBudgetPredictor 创建一个新的 Token 预算预测器。
// maxHistory 为历史记录最大长度，若 <= 0 则使用默认值 100。
func NewTokenBudgetPredictor(maxHistory int) *TokenBudgetPredictor {
	if maxHistory <= 0 {
		maxHistory = 100
	}
	return &TokenBudgetPredictor{
		history:        make([]int, 0, maxHistory),
		maxHistorySize: maxHistory,
	}
}

// RecordUsage 记录实际的 token 使用量。
func (p *TokenBudgetPredictor) RecordUsage(tokens int) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.history = append(p.history, tokens)
	if len(p.history) > p.maxHistorySize {
		p.history = p.history[len(p.history)-p.maxHistorySize:]
	}
}

// PredictNext 基于历史平均预测下一次消耗。
// 历史为空时返回 0。
func (p *TokenBudgetPredictor) PredictNext() int {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.totalPredictions++

	if len(p.history) == 0 {
		p.lastPrediction = 0
		return 0
	}

	sum := 0
	for _, v := range p.history {
		sum += v
	}
	prediction := sum / len(p.history)
	p.lastPrediction = prediction
	return prediction
}

// RecordAccuracy 比较上次预测和实际值，误差 < 20% 算准确。
func (p *TokenBudgetPredictor) RecordAccuracy(actual int) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.lastPrediction == 0 {
		if actual == 0 {
			p.accuratePredictions++
		}
		return
	}

	diff := tbpAbs(actual - p.lastPrediction)
	errorRatio := float64(diff) / float64(p.lastPrediction)
	if errorRatio < 0.2 {
		p.accuratePredictions++
	}
}

// GetStats 获取预测器的统计信息。
// 返回 totalPredictions、accuratePredictions、accuracy、avgUsage 和 lastPrediction。
func (p *TokenBudgetPredictor) GetStats() map[string]interface{} {
	p.mu.RLock()
	defer p.mu.RUnlock()

	stats := map[string]interface{}{
		"totalPredictions":    p.totalPredictions,
		"accuratePredictions": p.accuratePredictions,
		"lastPrediction":      p.lastPrediction,
	}

	if p.totalPredictions > 0 {
		stats["accuracy"] = float64(p.accuratePredictions) / float64(p.totalPredictions)
	} else {
		stats["accuracy"] = 0.0
	}

	if len(p.history) > 0 {
		sum := 0
		for _, v := range p.history {
			sum += v
		}
		stats["avgUsage"] = float64(sum) / float64(len(p.history))
	} else {
		stats["avgUsage"] = 0.0
	}

	return stats
}

// Reset 重置预测器的所有状态。
func (p *TokenBudgetPredictor) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.history = make([]int, 0, p.maxHistorySize)
	p.totalPredictions = 0
	p.accuratePredictions = 0
	p.lastPrediction = 0
}

// tbpAbs 返回整数的绝对值。
func tbpAbs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

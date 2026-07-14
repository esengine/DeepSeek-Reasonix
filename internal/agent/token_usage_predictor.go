package agent

import "sync"

// ── OPT-85: TokenUsagePredictor ──
// Predicts future token usage based on conversation patterns. By tracking the
// growth rate of token consumption across turns, it can forecast how many tokens
// will be needed in the next turn or several turns ahead, allowing the agent to
// proactively manage context before hitting limits.
//
// 原理：对话的 token 增长通常具有趋势性。TokenUsagePredictor 记录每轮
// 的实际 token 消耗，计算平均增长率，并据此预测未来 N 轮的 token 需求。
// 同时通过比较预测值和实际值来持续校准预测准确度。
//
// 效果：提前 1-3 轮预知 token 压力，使压缩决策从被动响应变为主动预防。

// UsageRecord stores the actual and predicted token usage for a single turn.
type UsageRecord struct {
	Turn            int
	ActualTokens    int
	PredictedTokens int
}

// TokenPredictorStats holds aggregated statistics about prediction activity.
type TokenPredictorStats struct {
	TotalPredictions int
	PredictionCount  int
	AvgAccuracy      float64
	HistorySize      int
}

// TokenUsagePredictor forecasts future token consumption based on historical
// growth patterns.
type TokenUsagePredictor struct {
	mu               sync.RWMutex
	usageHistory     []UsageRecord
	predictions      map[int]int
	totalPredictions int
	accuracySum      float64
	predictionCount  int
}

// NewTokenUsagePredictor creates a new TokenUsagePredictor.
func NewTokenUsagePredictor() *TokenUsagePredictor {
	return &TokenUsagePredictor{
		predictions: make(map[int]int),
	}
}

// RecordUsage records the actual token usage for a turn and, if a prediction
// was previously made for that turn, updates the prediction accuracy.
func (p *TokenUsagePredictor) RecordUsage(turn int, actualTokens int) {
	p.mu.Lock()
	defer p.mu.Unlock()

	predictedTokens := 0
	if predicted, ok := p.predictions[turn]; ok {
		predictedTokens = predicted

		// Calculate accuracy as 1 - |error| / actual.
		if actualTokens > 0 {
			diff := predicted - actualTokens
			if diff < 0 {
				diff = -diff
			}
			accuracy := 1.0 - float64(diff)/float64(actualTokens)
			if accuracy < 0 {
				accuracy = 0
			}
			p.accuracySum += accuracy
			p.predictionCount++
		}

		delete(p.predictions, turn)
	}

	p.usageHistory = append(p.usageHistory, UsageRecord{
		Turn:            turn,
		ActualTokens:    actualTokens,
		PredictedTokens: predictedTokens,
	})
}

// PredictNextTurn predicts the token usage for the next turn based on the
// average growth rate observed in the usage history. The prediction is stored
// so that accuracy can be evaluated when the actual usage is recorded.
func (p *TokenUsagePredictor) PredictNextTurn(currentTurn int, currentTokens int) int {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.totalPredictions++

	avgGrowth := p.calculateGrowthRate()

	predicted := int(float64(currentTokens) * avgGrowth)
	if predicted < 0 {
		predicted = 0
	}

	nextTurn := currentTurn + 1
	p.predictions[nextTurn] = predicted

	return predicted
}

// PredictInNTurns predicts the token usage n turns ahead by compounding the
// average growth rate.
func (p *TokenUsagePredictor) PredictInNTurns(currentTurn int, currentTokens int, n int) int {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.totalPredictions++

	if n <= 0 {
		return currentTokens
	}

	avgGrowth := p.calculateGrowthRate()

	predicted := currentTokens
	for i := 0; i < n; i++ {
		predicted = int(float64(predicted) * avgGrowth)
	}
	if predicted < 0 {
		predicted = 0
	}

	targetTurn := currentTurn + n
	p.predictions[targetTurn] = predicted

	return predicted
}

// calculateGrowthRate returns the average per-turn growth rate derived from the
// usage history. If there is insufficient history it defaults to 1.1 (10%
// growth). The caller must hold the write lock.
func (p *TokenUsagePredictor) calculateGrowthRate() float64 {
	if len(p.usageHistory) < 2 {
		return 1.1
	}

	totalGrowth := 0.0
	count := 0
	for i := 1; i < len(p.usageHistory); i++ {
		prev := p.usageHistory[i-1].ActualTokens
		curr := p.usageHistory[i].ActualTokens
		if prev > 0 {
			growth := float64(curr) / float64(prev)
			totalGrowth += growth
			count++
		}
	}

	if count == 0 {
		return 1.1
	}

	return totalGrowth / float64(count)
}

// GetPredictionAccuracy returns the average accuracy of all evaluated
// predictions. If no predictions have been evaluated it returns 0.
func (p *TokenUsagePredictor) GetPredictionAccuracy() float64 {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.predictionCount == 0 {
		return 0
	}
	return p.accuracySum / float64(p.predictionCount)
}

// GetStats returns aggregated statistics about prediction activity.
func (p *TokenUsagePredictor) GetStats() TokenPredictorStats {
	p.mu.RLock()
	defer p.mu.RUnlock()

	avgAccuracy := 0.0
	if p.predictionCount > 0 {
		avgAccuracy = p.accuracySum / float64(p.predictionCount)
	}

	return TokenPredictorStats{
		TotalPredictions: p.totalPredictions,
		PredictionCount:  p.predictionCount,
		AvgAccuracy:      avgAccuracy,
		HistorySize:      len(p.usageHistory),
	}
}

// Reset clears all usage history, predictions, and accumulated statistics.
func (p *TokenUsagePredictor) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.usageHistory = nil
	p.predictions = make(map[int]int)
	p.totalPredictions = 0
	p.accuracySum = 0
	p.predictionCount = 0
}

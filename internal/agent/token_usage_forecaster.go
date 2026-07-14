package agent

import "sync"

// ── OPT-126: TokenUsageForecaster (Token 使用量预测器) ──
// 基于历史 token 使用量的线性趋势，预测未来若干轮的 token 消耗。
// 通过记录每轮的实际使用量，计算最近若干个数据点的平均水位和平均增量，
// 据此线性外推未来 N 轮的 token 需求。同时支持评估预测准确性，
// 误差小于 25% 视为准确预测。
//
// 原理：对话中的 token 消耗通常呈现趋势性增长。TokenUsageForecaster
// 维护一个有限长度的使用历史窗口，提取最近 min(5, len) 个点的平均水位
// 与平均斜率，对未来轮次进行线性外推。
//
// 效果：提前预知未来几轮的 token 压力，为压缩和预算决策提供依据。

// TokenUsageForecaster 预测未来几轮的 token 使用量。
type TokenUsageForecaster struct {
	mu                sync.RWMutex
	usageHistory      []int
	forecastWindow    int
	totalForecasts    int
	accurateForecasts int
	maxHistorySize    int
	lastForecast      int // 最近一次 Forecast 的首轮预测值，用于 EvaluateForecast
}

// NewTokenUsageForecaster 创建一个新的 TokenUsageForecaster。
// maxHistory 指定历史记录最大长度，若 <= 0 则默认 50。
// forecastWindow 指定默认预测窗口大小。
func NewTokenUsageForecaster(maxHistory int, forecastWindow int) *TokenUsageForecaster {
	if maxHistory <= 0 {
		maxHistory = 50
	}
	if forecastWindow <= 0 {
		forecastWindow = 5
	}
	return &TokenUsageForecaster{
		maxHistorySize: maxHistory,
		forecastWindow: forecastWindow,
	}
}

// RecordUsage 记录一轮的实际 token 使用量到历史中。
// 当历史超过最大长度时，自动丢弃最旧的记录。
func (f *TokenUsageForecaster) RecordUsage(tokens int) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if tokens < 0 {
		tokens = 0
	}
	f.usageHistory = append(f.usageHistory, tokens)
	if len(f.usageHistory) > f.maxHistorySize {
		f.usageHistory = f.usageHistory[len(f.usageHistory)-f.maxHistorySize:]
	}
}

// Forecast 预测未来 rounds 轮的 token 使用量。
// 基于最近 min(5, len(history)) 个点的平均水位和平均增量进行线性外推。
// 返回长度为 rounds 的预测切片；同时将首轮预测值缓存用于后续评估。
func (f *TokenUsageForecaster) Forecast(rounds int) []int {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.totalForecasts++

	if rounds <= 0 {
		f.lastForecast = 0
		return []int{}
	}

	result := tufLinearForecast(f.usageHistory, rounds)
	if len(result) > 0 {
		f.lastForecast = result[0]
	} else {
		f.lastForecast = 0
	}
	return result
}

// EvaluateForecast 评估最近一次预测的准确性。
// 将实际使用量 actual 与缓存的首轮预测值比较，误差小于 25% 视为准确。
func (f *TokenUsageForecaster) EvaluateForecast(actual int) {
	f.mu.Lock()
	defer f.mu.Unlock()

	predicted := f.lastForecast
	if predicted <= 0 || actual <= 0 {
		return
	}

	diff := predicted - actual
	if diff < 0 {
		diff = -diff
	}
	errorRate := float64(diff) / float64(actual)
	if errorRate < 0.25 {
		f.accurateForecasts++
	}
}

// GetStats 返回预测器的统计信息。
func (f *TokenUsageForecaster) GetStats() map[string]interface{} {
	f.mu.RLock()
	defer f.mu.RUnlock()

	accuracy := 0.0
	if f.totalForecasts > 0 {
		accuracy = float64(f.accurateForecasts) / float64(f.totalForecasts)
	}

	avgUsage := 0
	if len(f.usageHistory) > 0 {
		sum := 0
		for _, v := range f.usageHistory {
			sum += v
		}
		avgUsage = sum / len(f.usageHistory)
	}

	return map[string]interface{}{
		"totalForecasts":    f.totalForecasts,
		"accurateForecasts": f.accurateForecasts,
		"accuracy":          accuracy,
		"avgUsage":          avgUsage,
		"historySize":       len(f.usageHistory),
	}
}

// Reset 清除所有历史记录和统计信息。
func (f *TokenUsageForecaster) Reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.usageHistory = nil
	f.totalForecasts = 0
	f.accurateForecasts = 0
	f.lastForecast = 0
}

// tufLinearForecast 基于历史数据的线性趋势预测未来 rounds 轮的 token 使用量。
// 使用最近 min(5, len(history)) 个点的平均水位和平均增量进行外推。
func tufLinearForecast(history []int, rounds int) []int {
	result := make([]int, 0, rounds)
	if rounds <= 0 {
		return result
	}

	if len(history) == 0 {
		for i := 0; i < rounds; i++ {
			result = append(result, 0)
		}
		return result
	}

	// 取最近 n 个点
	n := len(history)
	if n > 5 {
		n = 5
	}
	recent := history[len(history)-n:]

	// 平均水位
	sum := 0
	for _, v := range recent {
		sum += v
	}
	avgLevel := sum / n

	// 平均增量（斜率）
	avgDelta := 0
	if n >= 2 {
		deltaSum := 0
		for i := 1; i < len(recent); i++ {
			deltaSum += recent[i] - recent[i-1]
		}
		avgDelta = deltaSum / (n - 1)
	}

	for i := 0; i < rounds; i++ {
		forecast := avgLevel + avgDelta*(i+1)
		if forecast < 0 {
			forecast = 0
		}
		result = append(result, forecast)
	}
	return result
}

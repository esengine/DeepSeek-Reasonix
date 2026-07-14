package agent

import "sync"

// ── OPT-134: TokenCostProjector (Token 成本投影器) ──
// 投影未来对话的 token 成本。基于当前使用量、剩余轮次和平均每轮使用量
// 估算总 token 消耗，并乘以单价计算成本。同时通过记录实际使用量
// 来持续校准投影准确性。
//
// 原理：projectedTotal = currentUsage + remainingRounds * avgRoundUsage
//       projectedCost = projectedTotal * costPerToken
// 准确性 = 1 - |projected - actual| / projected

// TokenCostProjector Token 成本投影器，投影未来对话的 token 成本。
type TokenCostProjector struct {
	mu                   sync.RWMutex
	totalProjections     int
	totalProjectedTokens int
	totalActualTokens    int
	projectionAccuracy   float64
	costPerToken         float64
}

// NewTokenCostProjector 创建一个新的 Token 成本投影器实例。
// costPerToken 指定每个 token 的单价。
func NewTokenCostProjector(costPerToken float64) *TokenCostProjector {
	return &TokenCostProjector{
		costPerToken: costPerToken,
	}
}

// Project 投影未来对话的总 token 消耗和成本。
// projectedTotal = currentUsage + remainingRounds * avgRoundUsage
// projectedCost = projectedTotal * costPerToken
func (p *TokenCostProjector) Project(currentUsage int, remainingRounds int, avgRoundUsage int) (projectedTotal int, projectedCost float64) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.totalProjections++
	projectedTotal = currentUsage + remainingRounds*avgRoundUsage
	if projectedTotal < 0 {
		projectedTotal = 0
	}
	p.totalProjectedTokens += projectedTotal
	projectedCost = float64(projectedTotal) * p.costPerToken
	return
}

// RecordActual 记录实际 token 使用量，用于计算投影准确性。
func (p *TokenCostProjector) RecordActual(usage int) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.totalActualTokens += usage
	p.projectionAccuracy = tcpCalcAccuracy(p.totalProjectedTokens, p.totalActualTokens)
}

// GetCostEstimate 根据给定 token 数量估算成本: tokens * costPerToken。
func (p *TokenCostProjector) GetCostEstimate(tokens int) float64 {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return float64(tokens) * p.costPerToken
}

// GetStats 返回成本投影器的统计信息。
func (p *TokenCostProjector) GetStats() map[string]interface{} {
	p.mu.RLock()
	defer p.mu.RUnlock()

	avgProjectionCost := 0.0
	if p.totalProjections > 0 {
		avgProjectionCost = float64(p.totalProjectedTokens) * p.costPerToken / float64(p.totalProjections)
	}

	return map[string]interface{}{
		"totalProjections":     p.totalProjections,
		"totalProjectedTokens": p.totalProjectedTokens,
		"totalActualTokens":    p.totalActualTokens,
		"accuracy":             p.projectionAccuracy,
		"costPerToken":         p.costPerToken,
		"avgProjectionCost":    avgProjectionCost,
	}
}

// Reset 重置投影器的所有统计数据。
func (p *TokenCostProjector) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.totalProjections = 0
	p.totalProjectedTokens = 0
	p.totalActualTokens = 0
	p.projectionAccuracy = 0
}

// ---------------------------------------------------------------------------
// 辅助函数 (tcp 前缀)
// ---------------------------------------------------------------------------

// tcpCalcAccuracy 根据投影总量和实际总量计算准确性。
// 准确性 = 1 - |projected - actual| / projected，结果限制在 [0, 1]。
func tcpCalcAccuracy(projected, actual int) float64 {
	if projected == 0 {
		if actual == 0 {
			return 1.0
		}
		return 0
	}
	diff := projected - actual
	if diff < 0 {
		diff = -diff
	}
	accuracy := 1.0 - float64(diff)/float64(projected)
	if accuracy < 0 {
		accuracy = 0
	}
	if accuracy > 1 {
		accuracy = 1
	}
	return accuracy
}

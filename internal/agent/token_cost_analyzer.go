package agent

import "sync"

// ── OPT-87: Token 成本分析器 (Token Cost Analyzer) ──
// 分析整个对话的 token 成本，并识别可节省成本的机会。
//
// 原理：将输入、输出、缓存 token 分别计量，结合每百万 token 的单价
// 估算总成本与缓存带来的节省。同时基于用量模式识别可优化点，例如：
// 缓存利用率偏低、输出冗长、输入上下文过大等。
//
// 效果：量化 token 开销，定位成本优化方向，辅助决策是否触发压缩、
// 提升缓存命中率或控制输出长度。

// TokenCostAnalyzer Token 成本分析器
type TokenCostAnalyzer struct {
	mu                   sync.RWMutex
	totalInputTokens     int
	totalOutputTokens    int
	totalCacheTokens     int
	costPerMToken        float64
	savingsOpportunities []SavingsOpportunity
}

// SavingsOpportunity 节省成本的机会
type SavingsOpportunity struct {
	Category         string
	PotentialSavings int
	Description      string
}

// CostAnalysis 成本分析结果
type CostAnalysis struct {
	TotalCost     float64
	InputCost     float64
	OutputCost    float64
	CacheSavings  float64
	Opportunities []SavingsOpportunity
}

// TokenCostStats Token 成本统计
type TokenCostStats struct {
	TotalInputTokens  int
	TotalOutputTokens int
	TotalCacheTokens  int
	EstimatedCost     float64
}

// NewTokenCostAnalyzer 创建 Token 成本分析器。costPerMToken 为每百万 token 单价，
// 小于等于 0 时默认使用 1.0。
func NewTokenCostAnalyzer(costPerMToken float64) *TokenCostAnalyzer {
	if costPerMToken <= 0 {
		costPerMToken = 1.0
	}
	return &TokenCostAnalyzer{
		costPerMToken: costPerMToken,
	}
}

// RecordUsage 记录一次用量
func (a *TokenCostAnalyzer) RecordUsage(inputTokens, outputTokens, cacheTokens int) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.totalInputTokens += inputTokens
	a.totalOutputTokens += outputTokens
	a.totalCacheTokens += cacheTokens
}

// AnalyzeCosts 分析成本并识别节省机会
func (a *TokenCostAnalyzer) AnalyzeCosts() CostAnalysis {
	a.mu.Lock()
	defer a.mu.Unlock()

	inputCost := float64(a.totalInputTokens) / 1_000_000.0 * a.costPerMToken
	outputCost := float64(a.totalOutputTokens) / 1_000_000.0 * a.costPerMToken
	cacheSavings := float64(a.totalCacheTokens) / 1_000_000.0 * a.costPerMToken
	totalCost := inputCost + outputCost

	opportunities := a.identifyOpportunities()
	a.savingsOpportunities = opportunities

	return CostAnalysis{
		TotalCost:     totalCost,
		InputCost:     inputCost,
		OutputCost:    outputCost,
		CacheSavings:  cacheSavings,
		Opportunities: opportunities,
	}
}

// identifyOpportunities 基于当前用量识别节省机会（调用方需持锁）
func (a *TokenCostAnalyzer) identifyOpportunities() []SavingsOpportunity {
	var ops []SavingsOpportunity

	// 缓存利用率偏低：缓存 token 不足输入的 1/3
	if a.totalInputTokens > 0 && a.totalCacheTokens*3 < a.totalInputTokens {
		potential := a.totalInputTokens / 3 // 提升缓存可节省的输入 token 估算
		ops = append(ops, SavingsOpportunity{
			Category:         "cache_utilization",
			PotentialSavings: potential,
			Description:      "缓存利用率偏低，可通过稳定前缀提升缓存命中率以减少重复计费",
		})
	}

	// 输出冗长：输出 token 超过输入的一半
	if a.totalInputTokens > 0 && a.totalOutputTokens*2 > a.totalInputTokens {
		potential := a.totalOutputTokens / 5
		ops = append(ops, SavingsOpportunity{
			Category:         "verbose_output",
			PotentialSavings: potential,
			Description:      "输出 token 占比较高，可通过响应长度控制减少约 20% 输出成本",
		})
	}

	// 上下文过大：输入 token 超过 10 万
	if a.totalInputTokens > 100000 {
		potential := a.totalInputTokens * 3 / 10
		ops = append(ops, SavingsOpportunity{
			Category:         "large_context",
			PotentialSavings: potential,
			Description:      "输入上下文较大，可通过上下文压缩/裁剪减少约 30% 输入成本",
		})
	}

	return ops
}

// GetStats 返回 Token 成本统计
func (a *TokenCostAnalyzer) GetStats() TokenCostStats {
	a.mu.RLock()
	defer a.mu.RUnlock()

	totalTokens := a.totalInputTokens + a.totalOutputTokens
	estimatedCost := float64(totalTokens) / 1_000_000.0 * a.costPerMToken
	return TokenCostStats{
		TotalInputTokens:  a.totalInputTokens,
		TotalOutputTokens: a.totalOutputTokens,
		TotalCacheTokens:  a.totalCacheTokens,
		EstimatedCost:     estimatedCost,
	}
}

// Reset 重置分析器（保留单价配置）
func (a *TokenCostAnalyzer) Reset() {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.totalInputTokens = 0
	a.totalOutputTokens = 0
	a.totalCacheTokens = 0
	a.savingsOpportunities = nil
}

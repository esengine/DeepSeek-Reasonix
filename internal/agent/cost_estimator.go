package agent

import (
	"fmt"
	"sync"
	"time"
)

// ── OPT-28: Token 成本估算器 (Token Cost Estimator) ──
// 实时估算每个请求的 token 成本，提供经济感知的优化决策。
//
// 原理：不同类型的 token 有不同的成本：
// - 标准输入: 1x
// - 缓存写入: 1.25x (Anthropic) / 1x (OpenAI/DeepSeek)
// - 缓存读取: 0.1x (Anthropic) / 0.5x (OpenAI/DeepSeek)
// - 标准输出: 4-5x (比输入贵)
//
// 通过实时成本估算，可以：
// 1. 在请求前估算预期成本，如果过高则调整策略
// 2. 追踪累计成本，提供预算控制
// 3. 比较不同优化策略的成本效益
//
// 效果：提供精确到美分级的成本追踪，帮助用户和系统做出
// 经济最优的优化决策。

// CostEstimator token 成本估算器
type CostEstimator struct {
	mu sync.RWMutex

	// pricing 配置（每百万 token 的美元价格）
	pricing PricingConfig

	// 累计成本
	totalCost       float64
	totalInputCost  float64
	totalOutputCost float64
	totalCacheCost  float64

	// 按 turn 的成本历史
	turnCosts []TurnCost

	// 预算
	dailyBudget    float64 // 每日预算（美元）
	currentDaySpend float64
	budgetResetAt   time.Time

	// 统计
	requestCount int
}

// PricingConfig 定价配置
type PricingConfig struct {
	InputPerMillion     float64 `json:"inputPerMillion"`     // 标准输入价格
	OutputPerMillion    float64 `json:"outputPerMillion"`    // 标准输出价格
	CacheWritePerMillion float64 `json:"cacheWritePerMillion"` // 缓存写入价格
	CacheReadPerMillion  float64 `json:"cacheReadPerMillion"`  // 缓存读取价格
}

// TurnCost 单个 turn 的成本
type TurnCost struct {
	TurnID          string    `json:"turnId"`
	InputTokens     int       `json:"inputTokens"`
	OutputTokens    int       `json:"outputTokens"`
	CacheHitTokens  int       `json:"cacheHitTokens"`
	CacheMissTokens int       `json:"cacheMissTokens"`
	InputCost       float64   `json:"inputCost"`
	OutputCost      float64   `json:"outputCost"`
	CacheCost       float64   `json:"cacheCost"`
	TotalCost       float64   `json:"totalCost"`
	Timestamp       time.Time `json:"timestamp"`
}

// DefaultPricingConfigs 各 provider 的默认定价
var DefaultPricingConfigs = map[string]PricingConfig{
	"deepseek": {
		InputPerMillion:      0.27,
		OutputPerMillion:     1.10,
		CacheWritePerMillion: 0.27,
		CacheReadPerMillion:  0.07,
	},
	"openai": {
		InputPerMillion:      2.50,
		OutputPerMillion:     10.00,
		CacheWritePerMillion: 2.50,
		CacheReadPerMillion:  1.25,
	},
	"anthropic": {
		InputPerMillion:      3.00,
		OutputPerMillion:     15.00,
		CacheWritePerMillion: 3.75,
		CacheReadPerMillion:  0.30,
	},
	"gemini": {
		InputPerMillion:      1.25,
		OutputPerMillion:     5.00,
		CacheWritePerMillion: 1.25,
		CacheReadPerMillion:  0.3125,
	},
}

// NewCostEstimator 创建成本估算器
func NewCostEstimator(providerType string) *CostEstimator {
	pricing, ok := DefaultPricingConfigs[providerType]
	if !ok {
		pricing = DefaultPricingConfigs["deepseek"] // 默认用 DeepSeek 定价
	}
	return &CostEstimator{
		pricing: pricing,
	}
}

// EstimateCost 估算一次请求的成本
func (e *CostEstimator) EstimateCost(promptTokens, outputTokens, cacheHitTokens, cacheMissTokens int) TurnCost {
	e.mu.Lock()
	defer e.mu.Unlock()

	// 计算各部分成本
	standardInputTokens := promptTokens - cacheHitTokens - cacheMissTokens
	if standardInputTokens < 0 {
		standardInputTokens = 0
	}

	inputCost := float64(standardInputTokens) / 1_000_000 * e.pricing.InputPerMillion
	outputCost := float64(outputTokens) / 1_000_000 * e.pricing.OutputPerMillion
	cacheCost := float64(cacheMissTokens)/1_000_000*e.pricing.CacheWritePerMillion +
		float64(cacheHitTokens)/1_000_000*e.pricing.CacheReadPerMillion

	totalCost := inputCost + outputCost + cacheCost

	// 更新累计
	e.totalCost += totalCost
	e.totalInputCost += inputCost
	e.totalOutputCost += outputCost
	e.totalCacheCost += cacheCost
	e.currentDaySpend += totalCost
	e.requestCount++

	turnCost := TurnCost{
		InputTokens:     standardInputTokens,
		OutputTokens:    outputTokens,
		CacheHitTokens:  cacheHitTokens,
		CacheMissTokens: cacheMissTokens,
		InputCost:       inputCost,
		OutputCost:      outputCost,
		CacheCost:       cacheCost,
		TotalCost:       totalCost,
		Timestamp:       time.Now(),
	}

	e.turnCosts = append(e.turnCosts, turnCost)
	if len(e.turnCosts) > 100 {
		e.turnCosts = e.turnCosts[1:]
	}

	return turnCost
}

// EstimateSavings 估算缓存带来的节省金额
func (e *CostEstimator) EstimateSavings(cacheHitTokens, cacheMissTokens int) float64 {
	e.mu.RLock()
	defer e.mu.RUnlock()

	// 不使用缓存时，所有 token 按标准输入价格计费
	withoutCache := float64(cacheHitTokens+cacheMissTokens) / 1_000_000 * e.pricing.InputPerMillion

	// 使用缓存时的成本
	withCache := float64(cacheMissTokens)/1_000_000*e.pricing.CacheWritePerMillion +
		float64(cacheHitTokens)/1_000_000*e.pricing.CacheReadPerMillion

	savings := withoutCache - withCache
	if savings < 0 {
		return 0
	}
	return savings
}

// CheckBudget 检查预算
func (e *CostEstimator) CheckBudget() BudgetStatus {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.dailyBudget <= 0 {
		return BudgetStatusOK
	}

	usage := e.currentDaySpend / e.dailyBudget
	switch {
	case usage >= 1.0:
		return BudgetStatusExceeded
	case usage >= 0.80:
		return BudgetStatusCritical
	case usage >= 0.60:
		return BudgetStatusWarning
	default:
		return BudgetStatusOK
	}
}

// SetDailyBudget 设置每日预算
func (e *CostEstimator) SetDailyBudget(budget float64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.dailyBudget = budget
	e.budgetResetAt = time.Now().Truncate(24 * time.Hour).Add(24 * time.Hour)
}

// ResetDailyBudget 重置每日预算（在午夜调用）
func (e *CostEstimator) ResetDailyBudget() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if time.Now().After(e.budgetResetAt) {
		e.currentDaySpend = 0
		e.budgetResetAt = time.Now().Truncate(24 * time.Hour).Add(24 * time.Hour)
	}
}

// GetStats 获取统计
func (e *CostEstimator) GetStats() CostStats {
	e.mu.RLock()
	defer e.mu.RUnlock()

	return CostStats{
		TotalCost:       e.totalCost,
		TotalInputCost:  e.totalInputCost,
		TotalOutputCost: e.totalOutputCost,
		TotalCacheCost:  e.totalCacheCost,
		DailySpend:      e.currentDaySpend,
		DailyBudget:     e.dailyBudget,
		RequestCount:    e.requestCount,
		AvgCostPerRequest: func() float64 {
			if e.requestCount == 0 {
				return 0
			}
			return e.totalCost / float64(e.requestCount)
		}(),
		Pricing: e.pricing,
	}
}

// BudgetStatus 预算状态
type BudgetStatus int

const (
	BudgetStatusOK BudgetStatus = iota
	BudgetStatusWarning
	BudgetStatusCritical
	BudgetStatusExceeded
)

// CostStats 成本统计
type CostStats struct {
	TotalCost         float64       `json:"totalCost"`
	TotalInputCost    float64       `json:"totalInputCost"`
	TotalOutputCost   float64       `json:"totalOutputCost"`
	TotalCacheCost    float64       `json:"totalCacheCost"`
	DailySpend        float64       `json:"dailySpend"`
	DailyBudget       float64       `json:"dailyBudget"`
	RequestCount      int           `json:"requestCount"`
	AvgCostPerRequest float64       `json:"avgCostPerRequest"`
	Pricing           PricingConfig `json:"pricing"`
}

// FormatCost 格式化成本为可读字符串
func FormatCost(cost float64) string {
	if cost < 0.01 {
		return fmt.Sprintf("$%.4f", cost)
	}
	if cost < 1.0 {
		return fmt.Sprintf("$%.2f", cost)
	}
	return fmt.Sprintf("$%.2f", cost)
}

package agent

import "sync"

// ── OPT-143: TokenSavingsCalculator (Token 节省计算器) ──
// 记录并统计各优化策略的 token 节省量。每次记录时累计策略级
// 和全局级节省量，同时将记录存入历史列表（超出上限时截断旧记录）。
// 支持按策略查询、总节省查询和节省率计算。

// SavingsRecord 表示一次节省记录。
type SavingsRecord struct {
	Strategy string
	Original int
	Saved    int
}

// TokenSavingsCalculator Token 节省计算器，计算各优化策略的 token 节省量。
type TokenSavingsCalculator struct {
	mu             sync.RWMutex
	strategies     map[string]int
	totalSaved     int
	totalOriginal  int
	savingsHistory []SavingsRecord
	maxHistorySize int
}

// NewTokenSavingsCalculator 创建一个新的 Token 节省计算器。
// 默认 maxHistorySize 为 50。
func NewTokenSavingsCalculator() *TokenSavingsCalculator {
	return &TokenSavingsCalculator{
		strategies:     make(map[string]int),
		savingsHistory: make([]SavingsRecord, 0),
		maxHistorySize: 50,
	}
}

// RecordSavings 记录一次节省。
// 累计策略级节省量、全局节省量和原始 token 量，
// 并将记录追加到历史列表（超出 maxHistorySize 时截断旧记录）。
func (c *TokenSavingsCalculator) RecordSavings(strategy string, original int, saved int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.strategies[strategy] += saved
	c.totalSaved += saved
	c.totalOriginal += original

	c.savingsHistory = append(c.savingsHistory, SavingsRecord{
		Strategy: strategy,
		Original: original,
		Saved:    saved,
	})

	if len(c.savingsHistory) > c.maxHistorySize {
		c.savingsHistory = c.savingsHistory[len(c.savingsHistory)-c.maxHistorySize:]
	}
}

// GetSavingsByStrategy 获取指定策略的总节省量。
// 若策略不存在则返回 0。
func (c *TokenSavingsCalculator) GetSavingsByStrategy(strategy string) int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.strategies[strategy]
}

// GetTotalSavings 获取所有策略的总节省量。
func (c *TokenSavingsCalculator) GetTotalSavings() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.totalSaved
}

// GetSavingsRate 获取节省率 (totalSaved / totalOriginal)。
// 若 totalOriginal 为 0 则返回 0。
func (c *TokenSavingsCalculator) GetSavingsRate() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.totalOriginal == 0 {
		return 0
	}
	return float64(c.totalSaved) / float64(c.totalOriginal)
}

// GetStats 返回计算器的统计信息。
// 包含: totalSaved, totalOriginal, savingsRate, strategyCount, topStrategy。
func (c *TokenSavingsCalculator) GetStats() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var savingsRate float64
	if c.totalOriginal == 0 {
		savingsRate = 0
	} else {
		savingsRate = float64(c.totalSaved) / float64(c.totalOriginal)
	}

	topStrategy := tsc2FindTopStrategy(c.strategies)
	return map[string]interface{}{
		"totalSaved":    c.totalSaved,
		"totalOriginal": c.totalOriginal,
		"savingsRate":   savingsRate,
		"strategyCount": len(c.strategies),
		"topStrategy":   topStrategy,
	}
}

// Reset 重置计算器，清空所有策略记录和历史记录。
func (c *TokenSavingsCalculator) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.strategies = make(map[string]int)
	c.totalSaved = 0
	c.totalOriginal = 0
	c.savingsHistory = make([]SavingsRecord, 0)
}

// tsc2FindTopStrategy 查找节省量最大的策略名称。
// 若策略表为空则返回空字符串。
func tsc2FindTopStrategy(strategies map[string]int) string {
	topName := ""
	topSaved := 0
	for name, saved := range strategies {
		if saved > topSaved {
			topSaved = saved
			topName = name
		}
	}
	return topName
}

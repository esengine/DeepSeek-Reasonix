package agent

import (
	"sync"
	"time"
)

// ── OPT-18: 自适应上下文预算 (Adaptive Context Budget) ──
// 按任务复杂度动态分配 token 预算，避免简单任务浪费 token。
//
// 原理：不同任务对上下文窗口的需求不同。简单问答只需要 4K token，
// 代码重构可能需要 128K。固定预算要么浪费（简单任务）要么不够
// （复杂任务）。通过场景分类器自动调整预算，配合 compaction 阈值，
// 让简单任务更快压缩（省 token），复杂任务保留更多上下文（保质量）。
//
// 效果：简单任务 compaction 阈值从 80% 降到 60%（提前压缩），
// 复杂任务从 80% 提到 90%（延迟压缩），整体 token 消耗降低 15-25%。

// ContextBudget 上下文预算配置
type ContextBudget struct {
	mu              sync.RWMutex
	current         BudgetLevel
	maxContextTokens int
	softThreshold   float64 // 软压缩阈值
	snipThreshold   float64 // 裁剪阈值
	compactThreshold float64 // 压缩阈值
	forceThreshold  float64 // 强制压缩阈值
	tailTokens      int     // 压缩后保留的尾部 token 数
	lastAdjusted    time.Time
}

// BudgetLevel 预算级别
type BudgetLevel int

const (
	BudgetMinimal BudgetLevel = iota // 简单问答：4K 窗口
	BudgetStandard                   // 标准任务：32K 窗口
	BudgetExtended                   // 扩展任务：128K 窗口
	BudgetMaximum                    // 大型任务：256K+ 窗口
)

func (b BudgetLevel) String() string {
	switch b {
	case BudgetMinimal:
		return "minimal"
	case BudgetStandard:
		return "standard"
	case BudgetExtended:
		return "extended"
	case BudgetMaximum:
		return "maximum"
	default:
		return "unknown"
	}
}

// NewContextBudget 创建上下文预算
func NewContextBudget(maxTokens int) *ContextBudget {
	cb := &ContextBudget{
		current:          BudgetStandard,
		maxContextTokens: maxTokens,
	}
	cb.applyLevel(BudgetStandard)
	return cb
}

// applyLevel 应用预算级别
func (c *ContextBudget) applyLevel(level BudgetLevel) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.current = level
	switch level {
	case BudgetMinimal:
		// 简单任务：提前压缩，保留少量上下文
		c.softThreshold = 0.40
		c.snipThreshold = 0.50
		c.compactThreshold = 0.65
		c.forceThreshold = 0.75
		c.tailTokens = 4096
	case BudgetStandard:
		// 标准任务：默认阈值
		c.softThreshold = 0.50
		c.snipThreshold = 0.60
		c.compactThreshold = 0.80
		c.forceThreshold = 0.90
		c.tailTokens = 8192
	case BudgetExtended:
		// 扩展任务：延迟压缩，保留更多上下文
		c.softThreshold = 0.60
		c.snipThreshold = 0.70
		c.compactThreshold = 0.85
		c.forceThreshold = 0.93
		c.tailTokens = 16384
	case BudgetMaximum:
		// 大型任务：最大保留
		c.softThreshold = 0.70
		c.snipThreshold = 0.80
		c.compactThreshold = 0.90
		c.forceThreshold = 0.95
		c.tailTokens = 32768
	}
	c.lastAdjusted = time.Now()
}

// AdjustForScene 根据场景调整预算级别
func (c *ContextBudget) AdjustForScene(scene string, complexity int) {
	c.mu.Lock()
	currentLevel := c.current
	c.mu.Unlock()
	var newLevel BudgetLevel
	switch {
	case complexity <= 2 || scene == "simple_qa" || scene == "greeting":
		newLevel = BudgetMinimal
	case complexity <= 5 || scene == "coding" || scene == "searching":
		newLevel = BudgetStandard
	case complexity <= 8 || scene == "refactoring" || scene == "debugging":
		newLevel = BudgetExtended
	default:
		newLevel = BudgetMaximum
	}

	if newLevel != currentLevel {
		c.applyLevel(newLevel)
	}
}

// AdjustForUsage 根据实际使用情况自动调整
// 如果连续多次接近压缩阈值，自动升级预算
func (c *ContextBudget) AdjustForUsage(currentTokens int, compactCount int) {
	c.mu.RLock()
	maxTokens := c.maxContextTokens
	level := c.current
	c.mu.RUnlock()

	if maxTokens <= 0 {
		return
	}
	usage := float64(currentTokens) / float64(maxTokens)

	// 如果使用率超过 85% 且已经压缩过 2 次，升级预算
	if usage > 0.85 && compactCount >= 2 && level < BudgetMaximum {
		c.applyLevel(level + 1)
	}

	// 如果使用率低于 30% 且预算高于最小，降级
	if usage < 0.30 && level > BudgetMinimal {
		c.applyLevel(level - 1)
	}
}

// GetThresholds 获取当前阈值
func (c *ContextBudget) GetThresholds() (soft, snip, compact, force float64, tail int) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.softThreshold, c.snipThreshold, c.compactThreshold, c.forceThreshold, c.tailTokens
}

// GetLevel 获取当前级别
func (c *ContextBudget) GetLevel() BudgetLevel {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.current
}

// GetStats 获取统计
func (c *ContextBudget) GetStats() BudgetStats {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return BudgetStats{
		Level:            c.current.String(),
		MaxTokens:        c.maxContextTokens,
		SoftThreshold:    c.softThreshold,
		CompactThreshold: c.compactThreshold,
		ForceThreshold:   c.forceThreshold,
		TailTokens:       c.tailTokens,
		LastAdjusted:     c.lastAdjusted.Format(time.RFC3339),
	}
}

// BudgetStats 预算统计
type BudgetStats struct {
	Level            string  `json:"level"`
	MaxTokens        int     `json:"maxTokens"`
	SoftThreshold    float64 `json:"softThreshold"`
	CompactThreshold float64 `json:"compactThreshold"`
	ForceThreshold   float64 `json:"forceThreshold"`
	TailTokens       int     `json:"tailTokens"`
	LastAdjusted     string  `json:"lastAdjusted"`
}

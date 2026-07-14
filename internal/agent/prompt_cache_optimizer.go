package agent

import (
	"sync"
)

// ── OPT-77: 提示词缓存优化器 (PromptCacheOptimizer) ──
// 通过结构化提示词以最大化缓存复用来优化提示词缓存。
//
// 原理：将提示词拆分为稳定前缀（系统提示 + 早期上下文）
// 和可变后缀（近期消息 + 查询），使稳定前缀可以被缓存复用。
//
// 效果：缓存复用可减少 50-90% 的前缀 token 开销，
// 在多轮对话中效果尤为显著。

// CacheStructureInfo 缓存结构信息
type CacheStructureInfo struct {
	StablePrefix    string
	VariableSuffix  string
	PrefixTokens    int
	CacheableTokens int
}

// OptimizedPrompt 优化后的提示词
type OptimizedPrompt struct {
	StablePrefix         string
	VariablePart         string
	EstimatedCacheTokens int
	EstimatedSavings     int
}

// PromptCacheOptStats 提示词缓存优化统计快照
type PromptCacheOptStats struct {
	TotalOptimized   int
	TotalTokensSaved int
}

// PromptCacheOptimizer 提示词缓存优化器
type PromptCacheOptimizer struct {
	mu               sync.RWMutex
	totalOptimized   int
	totalTokensSaved int
	cacheStructure   map[string]*CacheStructureInfo
}

// NewPromptCacheOptimizer 创建新的提示词缓存优化器
func NewPromptCacheOptimizer() *PromptCacheOptimizer {
	return &PromptCacheOptimizer{
		cacheStructure: make(map[string]*CacheStructureInfo),
	}
}

// OptimizePrompt 将提示词拆分为稳定前缀和可变后缀以最大化缓存复用。
// 稳定前缀包含系统提示和早期上下文消息，可变后缀包含近期消息和用户查询。
func (p *PromptCacheOptimizer) OptimizePrompt(systemPrompt string, contextMessages []string, userQuery string) OptimizedPrompt {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Stable prefix: system prompt + early context messages (first half)
	stablePrefix := systemPrompt
	midPoint := len(contextMessages) / 2
	for i := 0; i < midPoint && i < len(contextMessages); i++ {
		stablePrefix += "\n" + contextMessages[i]
	}

	// Variable suffix: recent messages (second half) + user query
	variablePart := ""
	for i := midPoint; i < len(contextMessages); i++ {
		variablePart += contextMessages[i] + "\n"
	}
	variablePart += userQuery

	stableTokens := len(stablePrefix) / 4
	cacheableTokens := stableTokens
	// Assume ~50% savings from cache discount on stable prefix
	estimatedSavings := stableTokens / 2

	p.totalOptimized++
	p.totalTokensSaved += estimatedSavings

	// Cache the structure
	p.cacheStructure["last"] = &CacheStructureInfo{
		StablePrefix:    stablePrefix,
		VariableSuffix:  variablePart,
		PrefixTokens:    stableTokens,
		CacheableTokens: cacheableTokens,
	}

	return OptimizedPrompt{
		StablePrefix:         stablePrefix,
		VariablePart:         variablePart,
		EstimatedCacheTokens: stableTokens,
		EstimatedSavings:     estimatedSavings,
	}
}

// EstimateCacheSavings 估算缓存节省的 token 数。
// providerDiscount 为提供商提供的缓存折扣比例 (0.0-1.0)。
func (p *PromptCacheOptimizer) EstimateCacheSavings(stableTokens int, providerDiscount float64) int {
	return int(float64(stableTokens) * providerDiscount)
}

// GetStats 获取提示词缓存优化统计
func (p *PromptCacheOptimizer) GetStats() PromptCacheOptStats {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return PromptCacheOptStats{
		TotalOptimized:   p.totalOptimized,
		TotalTokensSaved: p.totalTokensSaved,
	}
}

// Reset 重置优化器状态
func (p *PromptCacheOptimizer) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.totalOptimized = 0
	p.totalTokensSaved = 0
	p.cacheStructure = make(map[string]*CacheStructureInfo)
}

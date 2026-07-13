package agent

import (
	"fmt"
	"sync"
)

// ── OPT-52: ProviderSpecificOptimizer — Provider 专属缓存优化器 ──
// 根据 DeepSeek/OpenAI/Anthropic/Gemini 的不同缓存策略优化 token 使用

// ProviderSpecConfig 保存 provider 专属优化配置
type ProviderSpecConfig struct {
	Type              ProviderType
	CacheStrategy     string  // "auto", "explicit", "none"
	CacheDiscount     float64 // 缓存 token 价格折扣 (0.5 = 50% off)
	MaxCachePoints    int     // 最大缓存断点数
	SupportsStreaming bool
	ContextWindow     int
}

// ProviderSpecResult 优化结果
type ProviderSpecResult struct {
	PotentialSavings       int
	CacheStrategy          string
	RecommendedCachePoints int
	OptimizationTips       []string
}

// ProviderSpecStats 统计信息
type ProviderSpecStats struct {
	TotalOptimized int
	TokensSaved    int
	CacheHitRate   float64
	ProviderType   string
}

// ProviderSpecificOptimizer provider 专属缓存优化器
type ProviderSpecificOptimizer struct {
	mu             sync.RWMutex
	providerType   ProviderType
	config         ProviderSpecConfig
	totalOptimized int
	tokensSaved    int
	cacheHitRate   float64
}

// NewProviderSpecificOptimizer 创建优化器
func NewProviderSpecificOptimizer(pt ProviderType) *ProviderSpecificOptimizer {
	pso := &ProviderSpecificOptimizer{providerType: pt}
	pso.config = pso.defaultConfig(pt)
	return pso
}

func (p *ProviderSpecificOptimizer) defaultConfig(pt ProviderType) ProviderSpecConfig {
	switch pt {
	case ProviderDeepSeek:
		return ProviderSpecConfig{Type: pt, CacheStrategy: "auto", CacheDiscount: 0.5, MaxCachePoints: 1, SupportsStreaming: true, ContextWindow: 128000}
	case ProviderAnthropic:
		return ProviderSpecConfig{Type: pt, CacheStrategy: "explicit", CacheDiscount: 0.9, MaxCachePoints: 4, SupportsStreaming: true, ContextWindow: 200000}
	case ProviderOpenAI:
		return ProviderSpecConfig{Type: pt, CacheStrategy: "auto", CacheDiscount: 0.5, MaxCachePoints: 1, SupportsStreaming: true, ContextWindow: 128000}
	case ProviderGemini:
		return ProviderSpecConfig{Type: pt, CacheStrategy: "explicit", CacheDiscount: 0.75, MaxCachePoints: 2, SupportsStreaming: true, ContextWindow: 1000000}
	default:
		return ProviderSpecConfig{Type: pt, CacheStrategy: "none", CacheDiscount: 0, MaxCachePoints: 0, SupportsStreaming: false, ContextWindow: 0}
	}
}

// OptimizeForProvider 计算基于 provider 缓存策略的潜在节省
func (p *ProviderSpecificOptimizer) OptimizeForProvider(promptTokens, cacheHitTokens, cacheMissTokens int) ProviderSpecResult {
	p.mu.Lock()
	defer p.mu.Unlock()

	var savings int
	var recommendedCachePoints int
	tips := []string{}
	cfg := p.config

	switch cfg.CacheStrategy {
	case "auto":
		savings = int(float64(cacheHitTokens) * (1 - cfg.CacheDiscount))
		recommendedCachePoints = 1
		tips = append(tips, fmt.Sprintf("Auto-cache: %d tokens at %.0f%% discount, saving ~%d", cacheHitTokens, (1-cfg.CacheDiscount)*100, savings))
		if cacheMissTokens > 0 {
			tips = append(tips, fmt.Sprintf("Reduce %d cache-miss tokens by keeping prefixes stable", cacheMissTokens))
		}
	case "explicit":
		recommendedCachePoints = cfg.MaxCachePoints
		if cacheHitTokens > 0 {
			savings = int(float64(cacheHitTokens) * (1 - cfg.CacheDiscount))
		}
		tips = append(tips, fmt.Sprintf("Explicit cache: use up to %d breakpoints for optimal savings", cfg.MaxCachePoints))
		if cacheMissTokens > 0 {
			tips = append(tips, fmt.Sprintf("Add breakpoints before %d cache-miss tokens", cacheMissTokens))
		}
	default:
		recommendedCachePoints = 0
		tips = append(tips, "This provider does not support prompt caching")
	}

	p.totalOptimized++
	p.tokensSaved += savings

	totalTokens := cacheHitTokens + cacheMissTokens
	if totalTokens > 0 {
		hitRate := float64(cacheHitTokens) / float64(totalTokens)
		if p.totalOptimized == 1 {
			p.cacheHitRate = hitRate
		} else {
			p.cacheHitRate = (p.cacheHitRate*float64(p.totalOptimized-1) + hitRate) / float64(p.totalOptimized)
		}
	}

	return ProviderSpecResult{
		PotentialSavings:       savings,
		CacheStrategy:          cfg.CacheStrategy,
		RecommendedCachePoints: recommendedCachePoints,
		OptimizationTips:       tips,
	}
}

// GetCacheRecommendation 返回 provider 专属缓存建议
func (p *ProviderSpecificOptimizer) GetCacheRecommendation() string {
	p.mu.RLock()
	defer p.mu.RUnlock()

	switch p.providerType {
	case ProviderDeepSeek:
		return "DeepSeek auto-caches prompt prefixes. Keep system prompts stable for cache hits."
	case ProviderAnthropic:
		return "Anthropic supports 4 explicit cache breakpoints. Place on system prompt, tools, context. 90% discount."
	case ProviderOpenAI:
		return "OpenAI auto-caches prompt prefixes. Keep prompts deterministic and ordered."
	case ProviderGemini:
		return "Gemini supports explicit cached content with 2 cache points. 75% discount."
	default:
		return "Provider caching support unknown."
	}
}

// GetStats 获取统计信息
func (p *ProviderSpecificOptimizer) GetStats() ProviderSpecStats {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return ProviderSpecStats{
		TotalOptimized: p.totalOptimized,
		TokensSaved:    p.tokensSaved,
		CacheHitRate:   p.cacheHitRate,
		ProviderType:   p.providerType.String(),
	}
}

// Reset 重置统计
func (p *ProviderSpecificOptimizer) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.totalOptimized = 0
	p.tokensSaved = 0
	p.cacheHitRate = 0
}

// SetProvider 切换 provider 类型
func (p *ProviderSpecificOptimizer) SetProvider(pt ProviderType) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.providerType = pt
	p.config = p.defaultConfig(pt)
}

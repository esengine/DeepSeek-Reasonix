package agent

import (
	"strings"
	"sync"
	"time"
)

// ── OPT-19: Provider 感知缓存策略 (Provider-Aware Cache Strategy) ──
// 按 provider 类型调整缓存行为，最大化各 provider 的缓存效率。
//
// 原理：不同 provider 的 prompt cache 机制差异显著：
// - DeepSeek: 自动前缀缓存，TTL ~1小时，无写入溢价，折扣 50%
// - OpenAI: 自动前缀缓存，TTL 5-10min，无写入溢价，折扣 50%
// - Anthropic: 显式断点控制，TTL 5min/1h，写入溢价 25%/100%，折扣 90%
// - Gemini: 显式缓存对象，TTL 5min-24h，按存储时长计费
//
// 根据 provider 类型自动调整：
// - 断点数量和位置
// - TTL 选择
// - 压缩策略（Anthropic 延迟压缩因为缓存收益大）
// - 工具 schema 压缩强度

// ProviderType provider 类型
type ProviderType int

const (
	ProviderUnknown ProviderType = iota
	ProviderDeepSeek
	ProviderOpenAI
	ProviderAnthropic
	ProviderGemini
	ProviderCustom
)

func (p ProviderType) String() string {
	switch p {
	case ProviderDeepSeek:
		return "deepseek"
	case ProviderOpenAI:
		return "openai"
	case ProviderAnthropic:
		return "anthropic"
	case ProviderGemini:
		return "gemini"
	case ProviderCustom:
		return "custom"
	default:
		return "unknown"
	}
}

// DetectProviderType 从模型名检测 provider 类型
func DetectProviderType(modelName string) ProviderType {
	m := strings.ToLower(modelName)
	switch {
	case strings.Contains(m, "deepseek"):
		return ProviderDeepSeek
	case strings.Contains(m, "gpt") || strings.Contains(m, "o1") || strings.Contains(m, "o3") || strings.Contains(m, "o4"):
		return ProviderOpenAI
	case strings.Contains(m, "claude") || strings.Contains(m, "anthropic"):
		return ProviderAnthropic
	case strings.Contains(m, "gemini") || strings.Contains(m, "google"):
		return ProviderGemini
	default:
		return ProviderCustom
	}
}

// CacheProfile 缓存配置文件
type CacheProfile struct {
	ProviderType       ProviderType
	MaxBreakpoints     int           // 最大断点数
	CacheReadDiscount  float64       // 缓存读取折扣（0.1 = 90% off）
	WritePremium       float64       // 写入溢价（0.25 = 25% extra）
	DefaultTTL         time.Duration // 默认 TTL
	MinCacheableTokens int           // 最小可缓存 token 数
	SupportsExplicitBP bool          // 是否支持显式断点
	AutoCache          bool          // 是否自动缓存
	CompactAggressively bool         // 是否激进压缩（缓存收益小时提前压缩）
}

// ProviderCacheStrategy provider 感知缓存策略
type ProviderCacheStrategy struct {
	mu       sync.RWMutex
	profile  CacheProfile
	provider ProviderType
}

// NewProviderCacheStrategy 创建缓存策略
func NewProviderCacheStrategy(providerType ProviderType) *ProviderCacheStrategy {
	profile := getCacheProfile(providerType)
	return &ProviderCacheStrategy{
		profile:  profile,
		provider: providerType,
	}
}

// getCacheProfile 获取 provider 对应的缓存配置
func getCacheProfile(pt ProviderType) CacheProfile {
	switch pt {
	case ProviderAnthropic:
		// Anthropic: 4 断点，90% 折扣，25% 写入溢价
		return CacheProfile{
			ProviderType:        pt,
			MaxBreakpoints:      4,
			CacheReadDiscount:   0.1,  // 90% off
			WritePremium:        0.25, // 25% extra for 5min TTL
			DefaultTTL:          5 * time.Minute,
			MinCacheableTokens:  1024,
			SupportsExplicitBP:  true,
			AutoCache:           false,
			CompactAggressively: false, // 缓存收益大，延迟压缩
		}

	case ProviderDeepSeek:
		// DeepSeek: 自动缓存，50% 折扣，无写入溢价
		return CacheProfile{
			ProviderType:        pt,
			MaxBreakpoints:      0, // 自动缓存，不需要断点
			CacheReadDiscount:   0.5, // 50% off
			WritePremium:        0.0, // 无写入溢价
			DefaultTTL:         60 * time.Minute, // ~1小时
			MinCacheableTokens: 256,
			SupportsExplicitBP: false,
			AutoCache:          true,
			CompactAggressively: true, // 缓存收益中等，适度提前压缩
		}

	case ProviderOpenAI:
		// OpenAI: 自动缓存，50% 折扣，无写入溢价
		return CacheProfile{
			ProviderType:        pt,
			MaxBreakpoints:      0, // 自动缓存
			CacheReadDiscount:   0.5,
			WritePremium:        0.0,
			DefaultTTL:          10 * time.Minute,
			MinCacheableTokens:  1024,
			SupportsExplicitBP:  false,
			AutoCache:           true,
			CompactAggressively: true,
		}

	case ProviderGemini:
		// Gemini: 显式缓存对象，75% 折扣，按时长计费
		return CacheProfile{
			ProviderType:        pt,
			MaxBreakpoints:      1, // 1 个缓存对象
			CacheReadDiscount:   0.25, // 75% off
			WritePremium:        0.0,  // 按存储时长计费
			DefaultTTL:          1 * time.Hour,
			MinCacheableTokens:  2048,
			SupportsExplicitBP:  true,
			AutoCache:           false,
			CompactAggressively: false,
		}

	default:
		// 未知 provider：保守策略
		return CacheProfile{
			ProviderType:        pt,
			MaxBreakpoints:      0,
			CacheReadDiscount:   1.0, // 无折扣
			WritePremium:        0.0,
			DefaultTTL:          0,
			MinCacheableTokens:  0,
			SupportsExplicitBP:  false,
			AutoCache:           false,
			CompactAggressively: true, // 保守，提前压缩
		}
	}
}

// GetProfile 获取缓存配置
func (s *ProviderCacheStrategy) GetProfile() CacheProfile {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.profile
}

// ShouldUseBreakpoints 是否应使用显式断点
func (s *ProviderCacheStrategy) ShouldUseBreakpoints() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.profile.SupportsExplicitBP && s.profile.MaxBreakpoints > 0
}

// GetBreakpointCount 获取建议的断点数
func (s *ProviderCacheStrategy) GetBreakpointCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.profile.MaxBreakpoints
}

// GetCompactThreshold 获取建议的压缩阈值
func (s *ProviderCacheStrategy) GetCompactThreshold() float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.profile.CompactAggressively {
		return 0.70 // 提前压缩
	}
	return 0.85 // 延迟压缩
}

// EstimateCacheSavings 估算缓存节省
func (s *ProviderCacheStrategy) EstimateCacheSavings(cacheHitTokens, cacheMissTokens int) int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// 节省 = 命中 token × (1 - 折扣) - 未命中 token × 写入溢价
	savings := int(float64(cacheHitTokens) * (1.0 - s.profile.CacheReadDiscount))
	cost := int(float64(cacheMissTokens) * s.profile.WritePremium)
	return savings - cost
}

// ShouldCompactNow 判断是否应该立即压缩
func (s *ProviderCacheStrategy) ShouldCompactNow(usage float64, compactCount int) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	threshold := s.profile.CompactAggressively
	if threshold {
		// 激进模式：60% 就开始压缩
		return usage > 0.60
	}
	// 保守模式：80% 才压缩
	return usage > 0.80
}

// GetCacheReadCost 获取缓存读取的相对成本
func (s *ProviderCacheStrategy) GetCacheReadCost() float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.profile.CacheReadDiscount
}

// SetProvider 更新 provider 类型
func (s *ProviderCacheStrategy) SetProvider(pt ProviderType) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.provider = pt
	s.profile = getCacheProfile(pt)
}

// GetStats 获取统计
func (s *ProviderCacheStrategy) GetStats() ProviderCacheStats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return ProviderCacheStats{
		Provider:           s.provider.String(),
		MaxBreakpoints:     s.profile.MaxBreakpoints,
		CacheReadDiscount:  s.profile.CacheReadDiscount,
		WritePremium:       s.profile.WritePremium,
		SupportsExplicitBP: s.profile.SupportsExplicitBP,
		AutoCache:          s.profile.AutoCache,
		CompactAggressively: s.profile.CompactAggressively,
	}
}

// ProviderCacheStats provider 缓存统计
type ProviderCacheStats struct {
	Provider            string  `json:"provider"`
	MaxBreakpoints      int     `json:"maxBreakpoints"`
	CacheReadDiscount   float64 `json:"cacheReadDiscount"`
	WritePremium        float64 `json:"writePremium"`
	SupportsExplicitBP  bool    `json:"supportsExplicitBP"`
	AutoCache           bool    `json:"autoCache"`
	CompactAggressively bool    `json:"compactAggressively"`
}

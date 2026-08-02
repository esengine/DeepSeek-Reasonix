package config

import (
	"strings"
	"time"
)

// DefaultCacheTTL returns the vendor's known prefix-cache retention based on
// the provider's base_url. Used by cold-resume prune to decide whether the
// provider cache is still warm after a session idle period.
//
// Values are deliberately conservative: too small burns a live cache (the user
// pays full price for a prefix that was still cached server-side), too large
// only forgoes a prune opportunity. Tighten from measured retention data.
func DefaultCacheTTL(baseURL string) time.Duration {
	switch detectCacheVendor(baseURL) {
	case "dashscope":
		// DashScope Session cache TTL is 5 minutes (documented).
		return 5 * time.Minute
	case "deepseek":
		// DeepSeek does not publish TTL; measured retention exceeds 1 hour.
		return 60 * time.Minute
	case "anthropic":
		// Anthropic ephemeral cache TTL is 5 minutes.
		return 5 * time.Minute
	default:
		// Unknown vendor: conservative 10 minutes.
		return 10 * time.Minute
	}
}

// EffectiveCacheTTL resolves the provider's cache TTL: an explicit
// cache_ttl_minutes config wins; otherwise the vendor default applies.
func (e *ProviderEntry) EffectiveCacheTTL() time.Duration {
	if e.CacheTTLMinutes > 0 {
		return time.Duration(e.CacheTTLMinutes) * time.Minute
	}
	return DefaultCacheTTL(e.BaseURL)
}

// detectCacheVendor identifies the provider vendor from its base_url for
// cache policy purposes. Mirrors provider/responses.DetectVendor but lives in
// the config layer to avoid an import cycle (control → config, not control →
// provider).
func detectCacheVendor(baseURL string) string {
	u := strings.ToLower(strings.TrimSpace(baseURL))
	switch {
	case strings.Contains(u, "dashscope.aliyuncs.com"), strings.Contains(u, ".maas.aliyuncs.com"):
		return "dashscope"
	case strings.Contains(u, "api.deepseek.com"):
		return "deepseek"
	case strings.Contains(u, "api.anthropic.com"):
		return "anthropic"
	default:
		return ""
	}
}

// usage.go — what a completion cost: the provider's token accounting and the
// price a vendor's rate puts on it.
package provider

import (
	"strings"
	"unicode"
)

// Usage reports token accounting for a completion. Cache hit/miss come from
// either DeepSeek's top-level prompt_cache_{hit,miss}_tokens or the OpenAI/MiMo
// standard prompt_tokens_details.cached_tokens — the openai provider normalises
// both shapes into these fields. ReasoningTokens is the thinking-mode subset of
// CompletionTokens reported by thinking-capable models. FinishReason carries
// the model's last reported choices[0].finish_reason so the agent can surface
// abnormal terminations ("length", "content_filter", "repetition_truncation").
// Estimated marks counts reconstructed locally because the provider's terminal
// usage record did not arrive; exact provider usage leaves it false.
type Usage struct {
	PromptTokens           int
	CompletionTokens       int
	TotalTokens            int
	CacheHitTokens         int     // prompt tokens served from cache
	CacheMissTokens        int     // prompt tokens not cached, including CacheWriteTokens
	CacheWriteTokens       int     // subset of CacheMissTokens used to create provider cache entries
	CacheWriteBilledTokens float64 // cache-write charge expressed in ordinary input-token equivalents
	ReasoningTokens        int     // subset of CompletionTokens spent on chain-of-thought
	FinishReason           string  // "stop", "tool_calls", "length", "content_filter", "repetition_truncation", …
	Estimated              bool
	// RequestCount is the number of provider requests represented by this
	// aggregate. Zero means one request for backward compatibility. Recovery
	// paths that merge multiple attempts set the exact count.
	RequestCount int
	// ServerToolRequests is how many tools the provider executed itself this
	// completion (Anthropic usage.server_tool_use.web_search_requests). Their
	// results enter the model's context without ever passing through us, so
	// PromptTokens on such a turn measures content we never held.
	ServerToolRequests int
	// Context* fields describe the latest single-request shape for context
	// gauges and rebind telemetry. When zero, consumers fall back to the
	// billable Prompt/Completion/… fields. Multi-attempt sampling recovery
	// sets PromptTokens (etc.) to the billable aggregate and fills Context*
	// from the final attempt only.
	ContextPromptTokens     int
	ContextCompletionTokens int
	ContextReasoningTokens  int
	ContextCacheHitTokens   int
	ContextCacheMissTokens  int
}

// ContextFillTokens returns the latest prompt occupancy used by context gauges.
func (u *Usage) ContextFillTokens() int {
	return u.LatestPromptTokens()
}

// LatestPromptTokens returns the latest-attempt prompt size for context-aware
// runtime decisions. Falls back to PromptTokens for single-attempt legacy usage.
func (u *Usage) LatestPromptTokens() int {
	if u == nil {
		return 0
	}
	if u.ContextPromptTokens > 0 {
		return u.ContextPromptTokens
	}
	return u.PromptTokens
}

// Pricing is a provider's per-1M-token rates, used to estimate spend. Currency
// is a display symbol or ISO-like code (default "¥"). toml tags let config decode it.
type Pricing struct {
	CacheHit float64 `toml:"cache_hit"` // per 1M cached prompt tokens
	Input    float64 `toml:"input"`     // per 1M uncached prompt tokens
	Output   float64 `toml:"output"`    // per 1M completion tokens
	Currency string  `toml:"currency"`
}

// Cost estimates the spend for a usage record. Compatibility adapter only —
// new host code must consume pricing.CostQuote instead of aggregating floats.
func (p *Pricing) Cost(u *Usage) float64 {
	if p == nil || u == nil {
		return 0
	}
	// Keep the historical float path byte-stable for tests that assert exact
	// float results without going through the fixed-point quote layer.
	hit := u.CacheHitTokens
	miss := u.CacheMissTokens
	if hit+miss == 0 && u.PromptTokens > 0 {
		miss = u.PromptTokens
	} else if miss == 0 && hit > 0 && u.PromptTokens > hit {
		miss = u.PromptTokens - hit
	}
	// CacheMissTokens intentionally remains the raw prompt-token denominator
	// used by cache hit-rate displays, so cache writes are included there. For
	// cost, split those writes back out and replace them with their provider-
	// supplied input-token equivalent (for example Anthropic's 1.25x 5-minute
	// writes or 2x 1-hour writes). Older providers leave both fields at zero and
	// keep the legacy one-input-rate behavior. A write count without billed
	// units also falls back to 1x for backward compatibility.
	write := min(max(u.CacheWriteTokens, 0), miss)
	billedWrite := 0.0
	if write > 0 {
		billedWrite = u.CacheWriteBilledTokens
		if billedWrite <= 0 {
			billedWrite = float64(write)
		}
	}
	inputTokenUnits := float64(miss-write) + billedWrite
	return (float64(hit)*p.CacheHit +
		inputTokenUnits*p.Input +
		float64(u.CompletionTokens)*p.Output) / 1e6
}

// Symbol returns the currency display symbol, defaulting to "¥".
func (p *Pricing) Symbol() string {
	if p == nil || p.Currency == "" {
		return "¥"
	}
	return currencySymbol(p.Currency)
}

func currencySymbol(currency string) string {
	value := strings.TrimSpace(currency)
	if value == "" {
		return "¥"
	}
	switch strings.ToLower(value) {
	case "cny", "rmb", "yuan", "renminbi", "cnh":
		return "¥"
	case "usd", "dollar", "dollars", "us dollar", "us dollars", "us$":
		return "$"
	case "eur", "euro", "euros":
		return "€"
	case "gbp", "pound", "pounds", "sterling":
		return "£"
	case "jpy", "yen":
		return "¥"
	}
	switch value {
	case "￥", "¥":
		return "¥"
	case "$", "€", "£":
		return value
	}
	// any embedded currency sign → keep as-is (compact symbols like A$, HK$).
	for _, r := range value {
		if unicode.Is(unicode.Sc, r) {
			return value
		}
	}
	if isThreeLetterCurrencyCode(value) {
		return strings.ToUpper(value) + " "
	}
	return "¥"
}

func isThreeLetterCurrencyCode(value string) bool {
	if len(value) != 3 {
		return false
	}
	for _, r := range value {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') {
			return false
		}
	}
	return true
}

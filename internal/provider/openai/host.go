package openai

import (
	"net/url"
	"strings"

	"reasonix/internal/provider"
)

// matchesVendorHost reports whether baseURL points at one of the canonical
// hostnames (exact match, case-insensitive) or at any subdomain of apex.
// Returns false on any parse error or empty host.
//
// We take the apex separately from the canonical because they differ: the
// canonical (e.g. api.minimaxi.com) is the specific endpoint, but regional
// subdomains like eu.minimaxi.com or us.minimaxi.com should also match —
// the wire shape is the same, just hosted in a different region. The bare
// apex (e.g. minimaxi.com) is intentionally rejected: it would only happen
// if the user pointed their base_url at the apex domain, which is a
// misconfiguration — not a path we want to silently accept.
func matchesVendorHost(baseURL, apex string, canonical ...string) bool {
	u, err := url.Parse(baseURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	for _, c := range canonical {
		if host == c {
			return true
		}
	}
	return strings.HasSuffix(host, "."+apex)
}

// IsDeepSeek reports whether baseURL points at DeepSeek's API
// (api.deepseek.com or any *.deepseek.com subdomain).
func IsDeepSeek(baseURL string) bool {
	return matchesVendorHost(baseURL, "deepseek.com", "api.deepseek.com")
}

// IsMiniMax reports whether baseURL points at MiniMax's OpenAI-compatible
// endpoint (api.minimaxi.com or any *.minimaxi.com subdomain).
//
// The host string is matched exactly — the spelling is `minimaxi`, not
// `minimax` — to avoid clashing with any future minimax-branded gateway.
func IsMiniMax(baseURL string) bool {
	return matchesVendorHost(baseURL, "minimaxi.com", "api.minimaxi.com")
}

// IsMiMo reports whether baseURL points at Xiaomi MiMo's OpenAI-compatible API.
// MiMo follows the OpenAI chat shape but authenticates with an `api-key` header
// instead of the usual Authorization bearer header.
func IsMiMo(baseURL string) bool {
	return provider.IsMiMoEndpoint(baseURL)
}

// IsZhipu reports whether baseURL points at Zhipu's OpenAI-compatible endpoint
// for GLM models — either the China host (open.bigmodel.cn, *.bigmodel.cn) or
// the international Z.ai host (api.z.ai, *.z.ai). Both speak the same wire shape,
// where chain-of-thought is gated by `thinking.type` (enabled|disabled) and
// `reasoning_effort` is silently ignored, so the client routes reasoning control
// to the thinking knob for either host.
func IsZhipu(baseURL string) bool {
	return matchesVendorHost(baseURL, "bigmodel.cn", "open.bigmodel.cn") ||
		matchesVendorHost(baseURL, "z.ai", "api.z.ai")
}

// IsLongCat reports whether baseURL points at LongCat's OpenAI-compatible API.
// LongCat uses the OpenAI chat shape, but gates thinking with thinking.type
// enabled|disabled rather than the generic reasoning_effort field.
func IsLongCat(baseURL string) bool {
	return matchesVendorHost(baseURL, "longcat.chat", "api.longcat.chat")
}

// IsOllamaCloud reports whether baseURL points at Ollama Cloud's hosted
// OpenAI-compatible endpoint. Local Ollama servers intentionally do not match:
// the hosted API accepts the reasoning_effort=max extension, while localhost
// deployments vary by model/version.
func IsOllamaCloud(baseURL string) bool {
	return matchesVendorHost(baseURL, "ollama.com", "ollama.com")
}

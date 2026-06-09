package openai

import (
	"net/url"
	"strings"
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

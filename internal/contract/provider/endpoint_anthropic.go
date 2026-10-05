package provider

import (
	"net/url"
	"strings"
)

// IsNativeAnthropic reports whether rawURL points at Anthropic's first-party
// Messages API. Compatible gateways answer the same paths without implementing
// the whole contract, so first-party-only behavior keys on this rather than on
// the "anthropic" provider kind. It lives here so the config layer can resolve
// an endpoint's reasoning contract without importing the client.
func IsNativeAnthropic(rawURL string) bool {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Hostname(), "api.anthropic.com")
}

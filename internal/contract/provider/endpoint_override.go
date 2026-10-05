package provider

import (
	"net/url"
	"strings"
)

// EndpointOverrideRepeatsBase reports whether an endpoint override only repeats
// one of bases, as editors that mirror base_url into every endpoint field leave
// it; the caller then derives its canonical route instead of POSTing to the
// API root. Scheme and host compare case-insensitively. An override carrying a
// query or fragment is deliberate (gateway tokens, debug routes) and stays.
func EndpointOverrideRepeatsBase(override string, bases ...string) bool {
	o, ok := parseEndpoint(override)
	if !ok || o.RawQuery != "" || o.Fragment != "" || o.ForceQuery {
		return false
	}
	for _, base := range bases {
		b, ok := parseEndpoint(base)
		if ok && strings.EqualFold(b.Scheme, o.Scheme) && strings.EqualFold(b.Host, o.Host) &&
			strings.TrimRight(b.EscapedPath(), "/") == strings.TrimRight(o.EscapedPath(), "/") {
			return true
		}
	}
	return false
}

func parseEndpoint(raw string) (*url.URL, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, false
	}
	return u, true
}

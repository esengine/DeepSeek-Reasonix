package mcpsetup

import (
	"maps"
	"net/url"
	"slices"
	"strings"
)

// Redact replaces a value whose key or content looks like a credential. It is
// what any surface printing an MCP config has to run first — a server block goes
// into bug reports and screenshots far more often than it gets read once.
func Redact(key, value string) string {
	if SensitiveKey(key) || SensitiveValue(value) {
		return "<redacted>"
	}
	return value
}

// SensitiveKey reports whether a config key names a credential.
func SensitiveKey(key string) bool {
	lower := strings.ToLower(strings.TrimSpace(key))
	for _, needle := range []string{"auth", "token", "secret", "credential", "api_key", "api-key", "apikey", "cookie", "password", "passwd"} {
		if strings.Contains(lower, needle) {
			return true
		}
	}
	return false
}

// SensitiveQueryKey adds the bare "key" query parameter, which is a credential
// in a URL and an ordinary word anywhere else.
func SensitiveQueryKey(key string) bool {
	return strings.EqualFold(strings.TrimSpace(key), "key") || SensitiveKey(key)
}

// SensitiveValue reports whether a value carries a credential regardless of the
// key it is filed under.
func SensitiveValue(value string) bool {
	lower := strings.ToLower(value)
	for _, needle := range []string{"access_token", "id_token", "refresh_token", "api_key", "api-key", "apikey", "bearer "} {
		if strings.Contains(lower, needle) {
			return true
		}
	}
	return false
}

// RedactURL masks userinfo, credential query values and credential fragments.
// Malformed endpoints, queries or fragments are fully masked.
func RedactURL(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return raw
	}
	u, err := url.Parse(trimmed)
	if err != nil || u == nil {
		return "<redacted>"
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return "<redacted>"
	}
	changed := false
	for key := range q {
		if SensitiveQueryKey(key) {
			q.Set(key, "<redacted>")
			changed = true
		}
	}
	if changed {
		u.RawQuery = q.Encode()
	}
	if u.User != nil {
		u.User = url.User("<redacted>")
		changed = true
	}
	fragment, err := url.ParseQuery(u.Fragment)
	if err != nil {
		return "<redacted>"
	}
	for key := range fragment {
		if SensitiveQueryKey(key) {
			u.Fragment = "<redacted>"
			u.RawFragment = ""
			changed = true
			break
		}
	}
	if !changed {
		return raw
	}
	return u.String()
}

func sortedKeys(m map[string]string) []string {
	keys := slices.Sorted(maps.Keys(m))
	return keys
}

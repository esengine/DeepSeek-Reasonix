package mcpinteraction

import (
	"net/url"
	"strings"
)

// allowedURL reports whether a server-provided elicitation URL may be shown to
// the user: http/https only, host present, and no userinfo or embedded
// credentials. The UI still shows server identity plus target domain and only
// opens the browser on explicit user action.
func allowedURL(raw string) bool {
	if strings.TrimSpace(raw) == "" {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
	default:
		return false
	}
	if u.User != nil {
		return false
	}
	if u.Host == "" {
		return false
	}
	if u.RawQuery != "" && strings.Contains(strings.ToLower(u.RawQuery), "password=") {
		return false
	}
	return true
}

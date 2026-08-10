package mcpdiag

import (
	"net"
	"net/url"
	"strings"
)

const (
	AuthNone     = "none"
	AuthPossible = "possible"
	AuthRequired = "required"
)

type AuthDiagnosis struct {
	Status string
	URL    string
}

func DiagnoseAuth(transport, status, errText, url string, authConfigured bool) AuthDiagnosis {
	eligible := CanUseHTTPMCPOAuth(transport, url, authConfigured)
	if IsAuthFailure(errText) {
		if eligible {
			return AuthDiagnosis{Status: AuthRequired, URL: strings.TrimSpace(url)}
		}
		return AuthDiagnosis{Status: AuthNone}
	}
	if !eligible || strings.TrimSpace(errText) != "" {
		return AuthDiagnosis{Status: AuthNone}
	}
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "connected", "failed":
		return AuthDiagnosis{Status: AuthNone}
	case "deferred", "initializing", "disabled":
		return AuthDiagnosis{Status: AuthPossible, URL: strings.TrimSpace(url)}
	default:
		return AuthDiagnosis{Status: AuthNone}
	}
}

// CanUseHTTPMCPOAuth reports whether Reasonix's native authorization-code flow
// can own authentication for this server. Legacy SSE, stdio, malformed URLs,
// and configurations with explicit credentials must keep their normal retry
// or credential-management path.
func CanUseHTTPMCPOAuth(transport, url string, authConfigured bool) bool {
	if authConfigured || HasAuthConfig(nil, nil, url) || !looksLikeNativeOAuthURL(url) {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(transport)) {
	case "http", "streamable-http", "streamable_http":
		return true
	default:
		return false
	}
}

// HTTPMCPOAuthResource returns the resource URL that native OAuth may own, or
// an empty string when the configured transport/auth boundary belongs elsewhere.
func HTTPMCPOAuthResource(transport, url string, authConfigured bool) string {
	if !CanUseHTTPMCPOAuth(transport, url, authConfigured) {
		return ""
	}
	return strings.TrimSpace(url)
}

func IsAuthFailure(errText string) bool {
	lower := strings.ToLower(errText)
	for _, needle := range []string{
		"401",
		"403",
		"unauthorized",
		"forbidden",
		"invalid token",
		"login required",
		"authentication",
		"not authenticated",
	} {
		if strings.Contains(lower, needle) {
			return true
		}
	}
	return false
}

func HasAuthConfig(headers, env map[string]string, url string) bool {
	for k, v := range headers {
		if strings.TrimSpace(k) == "" {
			continue
		}
		if strings.TrimSpace(v) != "" && (isAuthish(k) || containsExplicitAuthMaterial(v)) {
			return true
		}
	}
	if urlHasAuthConfig(url) {
		return true
	}
	for k, v := range env {
		if strings.TrimSpace(v) == "" {
			continue
		}
		if isAuthish(k) || containsAuthMaterial(v) {
			return true
		}
	}
	return false
}

func ClearAuthConfig(headers, env map[string]string, rawURL string) (map[string]string, map[string]string, string, bool) {
	cleanHeaders, changedHeaders := clearAuthMap(headers)
	cleanEnv, changedEnv := clearAuthMap(env)
	cleanURL, changedURL := clearAuthURL(rawURL)
	return cleanHeaders, cleanEnv, cleanURL, changedHeaders || changedEnv || changedURL
}

func IsRemoteTransport(transport string) bool {
	return isRemoteTransport(transport)
}

func isRemoteTransport(transport string) bool {
	switch strings.ToLower(strings.TrimSpace(transport)) {
	case "http", "streamable-http", "sse":
		return true
	default:
		return false
	}
}

func looksLikeHTTPURL(rawURL string) bool {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u == nil || strings.TrimSpace(u.Host) == "" {
		return false
	}
	return strings.EqualFold(u.Scheme, "https") || strings.EqualFold(u.Scheme, "http")
}

func looksLikeNativeOAuthURL(rawURL string) bool {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u == nil || strings.TrimSpace(u.Host) == "" || u.User != nil || u.Fragment != "" {
		return false
	}
	if strings.EqualFold(u.Scheme, "https") {
		return true
	}
	return strings.EqualFold(u.Scheme, "http") && isLoopbackHost(u.Hostname())
}

func isLoopbackHost(host string) bool {
	host = strings.Trim(strings.ToLower(strings.TrimSpace(host)), "[]")
	return host == "localhost" || (net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback())
}

func containsAuthMaterial(s string) bool {
	lower := strings.ToLower(s)
	return strings.Contains(lower, "${") || containsExplicitAuthMaterial(lower)
}

func containsExplicitAuthMaterial(s string) bool {
	lower := strings.ToLower(s)
	return strings.Contains(lower, "access_token") ||
		strings.Contains(lower, "id_token") ||
		strings.Contains(lower, "refresh_token") ||
		strings.Contains(lower, "api_key") ||
		strings.Contains(lower, "api-key") ||
		strings.Contains(lower, "apikey") ||
		strings.Contains(lower, "bearer ")
}

func isAuthish(key string) bool {
	lower := strings.ToLower(strings.TrimSpace(key))
	return strings.Contains(lower, "auth") ||
		strings.Contains(lower, "token") ||
		strings.Contains(lower, "secret") ||
		strings.Contains(lower, "credential") ||
		strings.Contains(lower, "api_key") ||
		strings.Contains(lower, "api-key") ||
		strings.Contains(lower, "apikey") ||
		strings.Contains(lower, "subscription-key") ||
		strings.Contains(lower, "subscription_key") ||
		strings.Contains(lower, "signature") ||
		strings.Contains(lower, "hmac") ||
		strings.Contains(lower, "cookie")
}

func clearAuthMap(in map[string]string) (map[string]string, bool) {
	if len(in) == 0 {
		return nil, false
	}
	out := make(map[string]string, len(in))
	changed := false
	for k, v := range in {
		if isAuthish(k) || containsExplicitAuthMaterial(v) {
			changed = true
			continue
		}
		out[k] = v
	}
	if len(out) == 0 {
		out = nil
	}
	return out, changed
}

func clearAuthURL(raw string) (string, bool) {
	trimmed := strings.TrimSpace(raw)
	if !looksLikeHTTPURL(trimmed) {
		return raw, false
	}
	u, err := url.Parse(trimmed)
	if err != nil || u == nil {
		return raw, false
	}
	q := u.Query()
	changed := u.User != nil
	u.User = nil
	for key := range q {
		if isAuthQueryKey(key) {
			q.Del(key)
			changed = true
		}
	}
	if !changed {
		return raw, false
	}
	u.RawQuery = q.Encode()
	return u.String(), true
}

func isAuthQueryKey(key string) bool {
	normalized := strings.NewReplacer("-", "", "_", "", " ", "").Replace(strings.ToLower(strings.TrimSpace(key)))
	switch normalized {
	case "auth", "authorization", "bearer", "credential", "credentials", "key", "sig", "signature", "hmac", "token", "accesstoken", "idtoken", "refreshtoken", "apikey", "accesskey", "secretkey", "subscriptionkey", "clientsecret", "password", "passwd":
		return true
	}
	for _, suffix := range []string{"token", "secret", "password", "passwd", "apikey", "signature"} {
		if strings.HasSuffix(normalized, suffix) {
			return true
		}
	}
	return false
}

func urlHasAuthConfig(raw string) bool {
	trimmed := strings.TrimSpace(raw)
	u, err := url.Parse(trimmed)
	if err == nil && u != nil {
		if u.User != nil {
			return true
		}
		for key := range u.Query() {
			if isAuthQueryKey(key) {
				return true
			}
		}
	}
	return containsAuthMaterial(trimmed)
}

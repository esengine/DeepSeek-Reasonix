package mcpdiag

import (
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
	if IsAuthFailure(errText) {
		return AuthDiagnosis{Status: AuthRequired, URL: remoteAuthURL(transport, url)}
	}
	if authConfigured || !isRemoteTransport(transport) || !looksLikeHTTPURL(url) || strings.TrimSpace(errText) != "" {
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
	if containsAuthMaterial(url) {
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

func remoteAuthURL(transport, url string) string {
	if !isRemoteTransport(transport) || !looksLikeHTTPURL(url) {
		return ""
	}
	return strings.TrimSpace(url)
}

func isRemoteTransport(transport string) bool {
	switch strings.ToLower(strings.TrimSpace(transport)) {
	case "http", "streamable-http", "sse":
		return true
	default:
		return false
	}
}

func looksLikeHTTPURL(url string) bool {
	u := strings.ToLower(strings.TrimSpace(url))
	return strings.HasPrefix(u, "https://") || strings.HasPrefix(u, "http://")
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
	changed := false
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
	lower := strings.ToLower(strings.TrimSpace(key))
	return lower == "key" || isAuthish(lower)
}

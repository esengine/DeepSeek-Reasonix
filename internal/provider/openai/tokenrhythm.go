package openai

import (
	"net/url"
	"strings"
)

const (
	tokenRhythmOrigin     = "https://tokenrhythm.studio"
	tokenRhythmHost       = "tokenrhythm.studio"
	tokenRhythmChatPath   = "/v1/chat/completions"
	tokenRhythmModelsPath = "/v1/models"
)

// canonicalTokenRhythmEndpoint rewrites known official Token Rhythm URLs.
// Official /v1 routes are already complete; unknown or unsafe URLs stay untouched.
func canonicalTokenRhythmEndpoint(raw, canonicalPath string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	if strings.ContainsAny(raw, "?#") {
		return "", false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.Opaque != "" {
		return "", false
	}
	if !strings.EqualFold(u.Hostname(), tokenRhythmHost) {
		return "", false
	}
	if port := u.Port(); port != "" && port != "443" {
		return "", false
	}
	if u.User != nil || u.RawPath != "" {
		return "", false
	}
	if !tokenRhythmKnownPath(u.Path) {
		return "", false
	}
	return tokenRhythmOrigin + canonicalPath, true
}

func canonicalTokenRhythmChatURL(raw string) (string, bool) {
	return canonicalTokenRhythmEndpoint(raw, tokenRhythmChatPath)
}

// CanonicalTokenRhythmModelsURL rewrites known official Token Rhythm URLs to GET /v1/models.
func CanonicalTokenRhythmModelsURL(raw string) (string, bool) {
	return canonicalTokenRhythmEndpoint(raw, tokenRhythmModelsPath)
}

func tokenRhythmKnownPath(path string) bool {
	if path != "/" {
		path = strings.TrimSuffix(path, "/")
	}
	switch path {
	case "", "/", "/v1", "/v1/v1":
		return true
	}
	for _, leaf := range []string{"/chat/completions", "/models"} {
		for _, prefix := range []string{"", "/v1", "/v1/v1"} {
			if path == prefix+leaf || path == prefix+leaf+leaf {
				return true
			}
		}
	}
	return false
}

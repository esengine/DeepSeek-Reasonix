package openai

import (
	"net/url"
	"strings"
)

// canonicalKnownVendorChatURL rewrites official-vendor bases whose documented
// form differs from the OpenAI-compatible shape (Token Rhythm, StepFun step_plan).
func canonicalKnownVendorChatURL(raw string) (string, bool) {
	if canonical, ok := canonicalTokenRhythmChatURL(raw); ok {
		return canonical, true
	}
	return canonicalStepFunPlanChatURL(raw)
}

// endpointOverrideMirrorsBase reports whether an endpoint override merely
// repeats the provider's own base URL. Provider editors that mirror base_url
// into every endpoint field produce exactly this shape. The mirrored value
// cannot be an endpoint: an OpenAI-compatible route is served from
// base_url + "/chat/completions", so honoring the override verbatim would POST
// to the API root and the gateway would answer "404 page not found".
//
// A query or fragment makes an override deliberate (gateway tokens, debug
// routes), so those stay user-owned and are never folded back into the base.
func endpointOverrideMirrorsBase(baseURL, override string) bool {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	raw := strings.TrimRight(strings.TrimSpace(override), "/")
	if base == "" || raw == "" {
		return false
	}
	baseParsed, err := url.Parse(base)
	if err != nil {
		return false
	}
	overrideParsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	if overrideParsed.RawQuery != "" || overrideParsed.Fragment != "" {
		return false
	}
	if !strings.EqualFold(baseParsed.Scheme, overrideParsed.Scheme) ||
		!strings.EqualFold(baseParsed.Host, overrideParsed.Host) {
		return false
	}
	return baseParsed.EscapedPath() == overrideParsed.EscapedPath()
}

func resolveOpenAIChatURL(baseURL string, extra map[string]any) string {
	requestURL, _ := extra["request_url"].(string)
	requestURL = strings.TrimSpace(requestURL)
	if canonical, ok := canonicalKnownVendorChatURL(requestURL); ok {
		return canonical
	}
	if requestURL != "" && !endpointOverrideMirrorsBase(baseURL, requestURL) {
		return requestURL
	}
	legacyChatURL, _ := extra["chat_url"].(string)
	return normalizeChatURL(baseURL, legacyChatURL)
}

func normalizeChatURL(baseURL, chatURL string) string {
	legacy := strings.TrimRight(strings.TrimSpace(chatURL), "/")
	if legacy != "" && !endpointOverrideMirrorsBase(baseURL, legacy) {
		if canonical, ok := canonicalKnownVendorChatURL(legacy); ok {
			return canonical
		}
		return legacy
	}
	if canonical, ok := canonicalKnownVendorChatURL(baseURL); ok {
		return canonical
	}
	return strings.TrimRight(strings.TrimSpace(baseURL), "/") + "/chat/completions"
}

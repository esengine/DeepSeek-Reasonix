package openai

import (
	"net/url"
	"strings"

	"reasonix/internal/contract/provider"
)

// visionReachesModel folds the host question into the model one, so a caller
// carries a single boolean instead of the pair.
func visionReachesModel(officialDeepSeek bool, model string) bool {
	return !officialDeepSeek || DeepSeekTakesImages(model)
}

// detailAccepted reports whether this image_url detail hint reaches the wire.
// "original" is DeepSeek's fourth level, measured 2026-08-21; OpenAI's API has
// no such variant, so widening the clamp for everyone would turn a setting that
// works into a rejected request. Anything else falls back to auto by omission.
func detailAccepted(detail string, officialDeepSeek bool) bool {
	switch detail {
	case "low", "high":
		return true
	case "original":
		return officialDeepSeek
	}
	return false
}

// deepSeekPrefixChatURL returns the official Beta chat endpoint that enables
// assistant-prefix completion. Derive it only from a URL already hosted by
// DeepSeek: custom gateways may opt into the DeepSeek reasoning wire shape, but
// must never be bypassed by an automatic request to the vendor's direct API.
func deepSeekPrefixChatURL(chatURL string) string {
	if !IsDeepSeek(chatURL) {
		return ""
	}
	u, err := url.Parse(strings.TrimSpace(chatURL))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	u.Path = "/beta/chat/completions"
	u.RawPath = ""
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

// usesGeminiThoughtSignatures reports whether the current endpoint/model speaks
// Gemini's OpenAI-compatible thought-signature extension. The official endpoint
// is authoritative even when a custom model alias is used; compatible gateways
// are detected from the model ID they route (for example google/gemini-3-pro).
// Keeping this decision on the current client prevents a Gemini-authored history
// from leaking extra_content.google fields after a same-session provider switch.
func usesGeminiThoughtSignatures(baseURL, model string) bool {
	if IsGeminiAPI(baseURL) {
		return true
	}
	for _, segment := range strings.FieldsFunc(strings.ToLower(strings.TrimSpace(model)), func(r rune) bool {
		return r == '/' || r == ':'
	}) {
		if segment == "gemini" || strings.HasPrefix(segment, "gemini-") || strings.HasPrefix(segment, "gemini_") {
			return true
		}
	}
	return false
}

// normalizeModelID converts Gemini's resource-form model names returned by some
// /models responses into the bare IDs required by OpenAI-compatible chat calls.
// Other providers and already-normalized Gemini IDs pass through unchanged.
func normalizeModelID(baseURL, model string) string {
	model = strings.TrimSpace(model)
	if IsGeminiAPI(baseURL) {
		model = strings.TrimPrefix(model, "models/")
	}
	return model
}

// IsMiMo reports whether baseURL points at Xiaomi MiMo's OpenAI-compatible API.
// MiMo follows the OpenAI chat shape but authenticates with an `api-key` header
// instead of the usual Authorization bearer header.
func IsMiMo(baseURL string) bool {
	return provider.IsMiMoEndpoint(baseURL)
}

// The vendor an endpoint belongs to is contract/provider's; these keep the
// names this package's wire code reads.
func IsDeepSeek(baseURL string) bool        { return provider.IsDeepSeekEndpoint(baseURL) }
func IsOpenAI(baseURL string) bool          { return provider.IsOpenAIEndpoint(baseURL) }
func IsGeminiAPI(baseURL string) bool       { return provider.IsGeminiEndpoint(baseURL) }
func IsMiniMax(baseURL string) bool         { return provider.IsMiniMaxEndpoint(baseURL) }
func IsZhipu(baseURL string) bool           { return provider.IsZhipuEndpoint(baseURL) }
func IsLongCat(baseURL string) bool         { return provider.IsLongCatEndpoint(baseURL) }
func IsKimiAPI(baseURL string) bool         { return provider.IsKimiEndpoint(baseURL) }
func IsOllamaCloud(baseURL string) bool     { return provider.IsOllamaCloudEndpoint(baseURL) }
func DeepSeekTakesImages(model string) bool { return provider.DeepSeekTakesImages(model) }

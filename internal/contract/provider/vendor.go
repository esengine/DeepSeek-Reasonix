// vendor.go — which vendor an endpoint belongs to, read from its address.
package provider

import (
	"net/url"
	"slices"
	"strings"
)

// matchVendorHost reports whether baseURL is one of the canonical hosts or any
// subdomain of apex, case-insensitively. Regional subdomains (eu.minimaxi.com)
// share the wire shape and match; the bare apex is not an API endpoint and does
// not.
func matchVendorHost(baseURL, apex string, canonical ...string) bool {
	u, err := url.Parse(baseURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if slices.Contains(canonical, host) {
		return true
	}
	return strings.HasSuffix(host, "."+apex)
}

// deepSeekImageModels are the official DeepSeek models that accept image input.
// Declared, not inferred: measured 2026-09-13 on both the OpenAI- and the
// Anthropic-shaped endpoint, the flash names answer from the pixels while pro
// takes the same image, returns 200 and answers as if it saw nothing. A refusal
// would have been evidence; a name is not, and nothing here spells vision.
var deepSeekImageModels = map[string]bool{
	"deepseek-flash":               true,
	"deepseek-v4-flash":            true,
	"deepseek-v4-flash-vision-exp": true,
}

// DeepSeekTakesImages reports whether this official DeepSeek model accepts
// image content. Callers pair it with IsDeepSeekEndpoint: the host alone stopped being
// the answer when one endpoint began serving both kinds.
func DeepSeekTakesImages(model string) bool {
	return deepSeekImageModels[strings.ToLower(strings.TrimSpace(model))]
}

// IsDeepSeekEndpoint reports whether baseURL points at DeepSeek's API
// (api.deepseek.com or any *.deepseek.com subdomain).
func IsDeepSeekEndpoint(baseURL string) bool {
	return matchVendorHost(baseURL, "deepseek.com", "api.deepseek.com")
}

// IsOpenAIEndpoint reports whether baseURL points at OpenAI's official API host. Keep
// this exact-host so a compatible gateway under another openai.com subdomain
// cannot accidentally receive the official max_completion_tokens wire shape.
func IsOpenAIEndpoint(baseURL string) bool {
	u, err := url.Parse(baseURL)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Hostname(), "api.openai.com")
}

// IsGeminiEndpoint reports whether baseURL points at Google's Gemini Developer API.
// Keep this exact-host: other googleapis.com services do not share Gemini's
// model resource-name compatibility quirk.
func IsGeminiEndpoint(baseURL string) bool {
	u, err := url.Parse(baseURL)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Hostname(), "generativelanguage.googleapis.com")
}

// IsMiniMaxEndpoint reports whether baseURL points at MiniMax's OpenAI-compatible
// endpoint (api.minimaxi.com or any *.minimaxi.com subdomain).
//
// The host string is matched exactly — the spelling is `minimaxi`, not
// `minimax` — to avoid clashing with any future minimax-branded gateway.
func IsMiniMaxEndpoint(baseURL string) bool {
	return matchVendorHost(baseURL, "minimaxi.com", "api.minimaxi.com")
}

// IsZhipuEndpoint reports whether baseURL is Zhipu's endpoint for GLM models,
// the China host (*.bigmodel.cn) or the international one (*.z.ai). Both gate
// thinking with thinking.type; GLM-5.2 and later also accept reasoning_effort
// (see ZhipuEffortContract for the per-model ladder).
func IsZhipuEndpoint(baseURL string) bool {
	return matchVendorHost(baseURL, "bigmodel.cn", "open.bigmodel.cn") ||
		matchVendorHost(baseURL, "z.ai", "api.z.ai")
}

// IsTokenRhythmEndpoint reports whether baseURL points at Token Rhythm's official
// OpenAI-compatible gateway. Keep this exact-host: model-aware protocol
// upgrades must not affect unrelated subdomains or similarly named relays.
func IsTokenRhythmEndpoint(baseURL string) bool {
	u, err := url.Parse(baseURL)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Hostname(), "tokenrhythm.studio")
}

// IsLongCatEndpoint reports whether baseURL points at LongCat's OpenAI-compatible API.
// LongCat uses the OpenAI chat shape, but gates thinking with thinking.type
// enabled|disabled rather than the generic reasoning_effort field.
func IsLongCatEndpoint(baseURL string) bool {
	return matchVendorHost(baseURL, "longcat.chat", "api.longcat.chat")
}

// IsKimiEndpoint reports whether baseURL is one of Moonshot's official Kimi direct
// API endpoints. Gate Kimi-specific wire compatibility on the exact API hosts
// so OpenAI-compatible relays carrying the same model ID remain untouched.
func IsKimiEndpoint(baseURL string) bool {
	u, err := url.Parse(baseURL)
	if err != nil {
		return false
	}
	switch strings.ToLower(u.Hostname()) {
	case "api.moonshot.cn", "api.moonshot.ai":
		return true
	default:
		return false
	}
}

// IsOllamaCloudEndpoint reports whether baseURL points at Ollama Cloud's hosted
// OpenAI-compatible endpoint. Local Ollama servers intentionally do not match:
// the hosted API accepts the reasoning_effort=max extension, while localhost
// deployments vary by model/version.
func IsOllamaCloudEndpoint(baseURL string) bool {
	return matchVendorHost(baseURL, "ollama.com", "ollama.com")
}

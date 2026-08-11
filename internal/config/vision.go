package config

import (
	"net/url"
	"slices"
	"strings"

	"reasonix/internal/provider/openai"
)

var mimoVisionModels = map[string]bool{
	"mimo-v2.5":    true,
	"mimo-v2-omni": true,
}

// InferVisionModels returns model IDs that look like chat models with image-input
// support. It is intentionally conservative and meant for Settings defaults; an
// explicit provider vision_models list remains the source of truth.
func InferVisionModels(models []string) []string {
	out := make([]string, 0, len(models))
	seen := map[string]bool{}
	for _, model := range models {
		model = strings.TrimSpace(model)
		if model == "" || seen[model] || !IsLikelyChatModel(model) || !IsLikelyVisionModel(model) {
			continue
		}
		seen[model] = true
		out = append(out, model)
	}
	return out
}

func IsLikelyVisionModel(model string) bool {
	model = strings.TrimSpace(model)
	if model == "" {
		return false
	}
	lower := strings.ToLower(model)
	if mimoVisionModels[lower] {
		return true
	}
	tokens := strings.FieldsFunc(lower, modelTokenSeparator)
	if slices.Contains(tokens, "audio") {
		return false
	}
	if strings.HasPrefix(lower, "gpt-4o") {
		return true
	}
	for _, token := range tokens {
		switch token {
		case "vl", "vision", "visual", "multimodal", "omni":
			return true
		}
	}
	return false
}

func modelTokenSeparator(r rune) bool {
	return r == '-' || r == '_' || r == '.' || r == '/' || r == ':'
}

// CanConfigureVision reports whether user-supplied vision metadata may enable
// image input for this endpoint. DeepSeek's official APIs currently accept text
// message content only, so their protocol constraint is authoritative over
// persisted provider-wide flags, vision_models, and model overrides. Custom
// DeepSeek-compatible gateways remain configurable because they may implement
// their own multimodal translation layer.
func CanConfigureVision(e *ProviderEntry) bool {
	return e != nil && !openai.IsDeepSeek(e.BaseURL)
}

// EffectiveVision resolves whether the selected model accepts image input.
// Explicit provider vision still wins for custom vision-capable gateways; the
// MiMo endpoint heuristic is deliberately limited to known MiMo endpoints so
// arbitrary OpenAI-compatible proxies do not get image payloads unexpectedly.
func EffectiveVision(e *ProviderEntry) bool {
	if !CanConfigureVision(e) {
		return false
	}
	if enabled, explicit := explicitModelVision(e); explicit {
		return enabled
	}
	if e.Vision {
		return true
	}
	return isOfficialMimoVisionEntry(e)
}

// ExplicitModelVision reports whether the selected model has an explicit,
// positive image capability declaration that the endpoint is allowed to use.
// Keep this query separate from EffectiveVision so callers can distinguish a
// model-scoped capability from provider-wide or endpoint-inferred support.
func ExplicitModelVision(e *ProviderEntry) bool {
	if !CanConfigureVision(e) {
		return false
	}
	enabled, explicit := explicitModelVision(e)
	return explicit && enabled
}

func explicitModelVision(e *ProviderEntry) (enabled, explicit bool) {
	if e == nil {
		return false, false
	}
	if e.visionOverride != nil {
		return *e.visionOverride, true
	}
	if e.HasVisionModel(e.Model) {
		return true, true
	}
	return false, false
}

func (e *ProviderEntry) HasVisionModel(model string) bool {
	model = strings.TrimSpace(model)
	if model == "" {
		return false
	}
	for _, candidate := range e.VisionModels {
		if strings.EqualFold(strings.TrimSpace(candidate), model) {
			return true
		}
	}
	return false
}

func isOfficialMimoVisionEntry(e *ProviderEntry) bool {
	if !isOpenAIProviderKind(e) || !mimoVisionModels[strings.ToLower(strings.TrimSpace(e.Model))] {
		return false
	}
	switch officialMimoHost(e.BaseURL) {
	case "api.xiaomimimo.com", "token-plan-cn.xiaomimimo.com":
		return true
	default:
		return false
	}
}

func officialMimoHost(baseURL string) string {
	u, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

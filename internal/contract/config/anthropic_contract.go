package config

import (
	"fmt"
	"strings"

	"reasonix/internal/contract/provider"
)

// isAnthropicDepthEntry reports whether an Anthropic-kind endpoint implements
// Anthropic's own extended-thinking contract — adaptive thinking plus
// output_config.effort. Nothing on the wire separates a gateway that
// reimplemented it from one that answers 5xx for fields it never heard of, so
// outside first-party endpoints it takes a declaration.
func isAnthropicDepthEntry(e *ProviderEntry) bool {
	if e == nil || !strings.EqualFold(strings.TrimSpace(e.Kind), "anthropic") {
		return false
	}
	if provider.IsDeepSeekEndpoint(e.BaseURL) {
		// DeepSeek's Anthropic-compatible endpoint has depth of its own, on a
		// different dialect (thinking.type=enabled with output_config.effort).
		return false
	}
	if isNativeAnthropicBaseURL(e.BaseURL) {
		return true
	}
	if len(depthEffortLevels(normalizedSupportedEfforts(e))) > 0 {
		return true
	}
	return curatedAdaptiveThinkingPreset(e)
}

// isNativeAnthropicBaseURL treats an unset base URL as first-party: that is the
// endpoint the Anthropic client defaults to.
func isNativeAnthropicBaseURL(raw string) bool {
	raw = strings.TrimSpace(raw)
	return raw == "" || provider.IsNativeAnthropic(raw)
}

// curatedAdaptiveThinkingPreset reports whether the entry came from a curated
// preset whose own definition asks for adaptive thinking. Curation is the
// declaration: a preset carries the mode a human confirmed against the vendor,
// and an installed entry records the preset it came from.
func curatedAdaptiveThinkingPreset(e *ProviderEntry) bool {
	id := strings.ToLower(strings.TrimSpace(e.PresetID))
	if id == "" {
		return false
	}
	for _, preset := range curatedProviderPresets {
		if preset.ID != id {
			continue
		}
		for _, entry := range preset.Entries {
			// Either identity is enough: a renamed provider and an edited base
			// URL are both still the endpoint the preset was vetted against.
			if strings.EqualFold(entry.Name, e.Name) || strings.EqualFold(entry.BaseURL, e.BaseURL) {
				return strings.EqualFold(strings.TrimSpace(entry.Thinking), "adaptive")
			}
		}
	}
	return false
}

// depthEffortLevels keeps only the levels that name a reasoning depth: an
// on/off toggle says nothing about whether an endpoint understands depth.
func depthEffortLevels(levels []string) []string {
	out := make([]string, 0, len(levels))
	for _, level := range levels {
		switch level {
		case "", "auto", "enabled", "adaptive", "disabled", "none", "off":
		default:
			out = append(out, level)
		}
	}
	return out
}

// anthropicEffortCapability is Anthropic's own extended-thinking menu: depth
// rides on output_config.effort, and "auto" leaves the model default in place.
func anthropicEffortCapability() EffortCapability {
	return EffortCapability{Supported: true, Levels: []string{"auto", "low", "medium", "high", "xhigh", "max"}, Default: "auto"}
}

func normalizeAnthropicEffort(level string) (string, error) {
	switch level {
	case "low", "medium", "high", "xhigh", "max":
		return level, nil
	default:
		return "", fmt.Errorf("usage: /effort auto|low|medium|high|xhigh|max")
	}
}

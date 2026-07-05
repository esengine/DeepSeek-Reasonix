package config

import (
	"fmt"
	"strings"

	"reasonix/internal/provider/openai"
)

const (
	ReasoningProtocolAuto     = "auto"
	ReasoningProtocolDeepSeek = "deepseek"
	ReasoningProtocolOpenAI   = "openai"
	ReasoningProtocolNone     = "none"
)

// EffortCapability describes the abstract effort levels a provider/model can set
// through the /effort command.
type EffortCapability struct {
	Supported bool
	Levels    []string
	Default   string
}

type modelReasoningCapability struct {
	Protocol string
	Levels   []string
	Default  string
}

var modelReasoningCapabilities = map[string]modelReasoningCapability{
	"deepseek-v4-flash": {Protocol: ReasoningProtocolDeepSeek, Levels: []string{"disabled", "high", "max"}, Default: "high"},
	"deepseek-v4-pro":   {Protocol: ReasoningProtocolDeepSeek, Levels: []string{"disabled", "high", "max"}, Default: "high"},
}

// EffortCapabilityForEntry returns the user-facing /effort levels for a resolved
// provider entry. Provider implementations still decide how a stored effort is
// serialized into requests.
func EffortCapabilityForEntry(e *ProviderEntry) EffortCapability {
	if explicitReasoningProtocol(e) == ReasoningProtocolNone {
		return EffortCapability{}
	}
	supported := normalizedSupportedEfforts(e)
	if len(supported) > 0 {
		levels := make([]string, 0, len(supported)+1)
		levels = append(levels, "auto")
		levels = append(levels, supported...)
		def := normalizeEffortLevel(e.DefaultEffort)
		if def == "" || !containsString(supported, def) {
			def = supported[0]
		}
		return EffortCapability{Supported: true, Levels: levels, Default: def}
	}
	switch explicitReasoningProtocol(e) {
	case ReasoningProtocolDeepSeek:
		return deepSeekEffortCapability()
	case ReasoningProtocolOpenAI:
		return openAIEffortCapability()
	}
	if cap, ok := resolvedModelReasoningCapability(e); ok {
		return effortCapabilityFromModel(cap)
	}
	switch ReasoningProtocolForEntry(e) {
	case ReasoningProtocolDeepSeek:
		return deepSeekEffortCapability()
	case ReasoningProtocolOpenAI:
		return openAIEffortCapability()
	}
	switch {
	case isMiniMaxEntry(e):
		// MiniMax-M3 only exposes a binary thinking knob (adaptive|disabled)
		// on its OpenAI-compatible endpoint, so /effort mirrors the API
		// vocabulary verbatim. Default is "adaptive" because the M3 model
		// runs with thinking on out of the box; "auto" means "don't override
		// the model default" (== adaptive for M3).
		return EffortCapability{Supported: true, Levels: []string{"auto", "adaptive", "disabled"}, Default: "adaptive"}
	case isZhipuEntry(e):
		// Zhipu GLM exposes a binary thinking knob (enabled|disabled) on its
		// OpenAI-compatible endpoint and ignores reasoning_effort, so /effort
		// mirrors that vocabulary. Default is "enabled" because GLM runs with
		// thinking on out of the box; "auto" means "don't override the model
		// default" (== enabled for GLM).
		return EffortCapability{Supported: true, Levels: []string{"auto", "enabled", "disabled"}, Default: "enabled"}
	case isLongCatEntry(e):
		// LongCat exposes the same binary thinking vocabulary on its
		// OpenAI-compatible endpoint and documents no reasoning_effort depth scale.
		return EffortCapability{Supported: true, Levels: []string{"auto", "enabled", "disabled"}, Default: "enabled"}
	case isOllamaCloudEntry(e):
		// Ollama Cloud accepts top-level reasoning_effort values low|medium|
		// high|max. "none" means omit the field so the hosted model runs without
		// thinking. Leave auto as the default so existing traffic stays provider-
		// default until the user chooses an effort explicitly.
		return EffortCapability{Supported: true, Levels: []string{"auto", "none", "low", "medium", "high", "max"}, Default: "auto"}
	case e != nil && e.Kind == "anthropic":
		return EffortCapability{Supported: true, Levels: []string{"auto", "low", "medium", "high", "xhigh", "max"}, Default: "auto"}
	default:
		return EffortCapability{}
	}
}

// NormalizeEffort maps a user-supplied /effort level into the value stored in
// config. Empty means auto/provider default.
func NormalizeEffort(e *ProviderEntry, raw string) (string, error) {
	level := normalizeEffortLevel(raw)
	if level == "" {
		return "", fmt.Errorf("usage: /effort auto|<level>")
	}
	if level == "auto" {
		return "", nil
	}
	if explicitReasoningProtocol(e) == ReasoningProtocolNone {
		return "", effortNotConfigurableError(e)
	}
	supported := normalizedSupportedEfforts(e)
	if len(supported) > 0 {
		if containsString(supported, level) {
			return level, nil
		}
		return "", fmt.Errorf("usage: /effort auto|%s", strings.Join(supported, "|"))
	}
	switch ReasoningProtocolForEntry(e) {
	case ReasoningProtocolDeepSeek:
		switch level {
		case "disabled":
			return "disabled", nil
		case "off": // retired DeepSeek "no thinking" → disabled
			return "disabled", nil
		case "high", "max":
			return level, nil
		case "low", "medium":
			return "high", nil
		case "xhigh":
			return "max", nil
		default:
			return "", fmt.Errorf("usage: /effort auto|disabled|high|max")
		}
	case ReasoningProtocolOpenAI:
		switch level {
		case "low", "medium", "high":
			return level, nil
		default:
			return "", fmt.Errorf("usage: /effort auto|low|medium|high")
		}
	}
	switch {
	case isMiniMaxEntry(e):
		// The M3 knob is binary; map Anthropic / OpenAI-style levels onto the
		// nearest valid value so a stale /effort high|low still works. "off"
		// is a retired DeepSeek level meaning "no thinking" — on M3 that maps
		// to "disabled" rather than the model default, since M3 actually
		// supports a "thinking off" mode and "off" is the natural request.
		switch level {
		case "adaptive", "disabled":
			return level, nil
		case "off":
			return "disabled", nil
		case "low", "medium", "high":
			return "adaptive", nil
		case "xhigh", "max":
			return "disabled", nil
		default:
			return "", fmt.Errorf("usage: /effort auto|adaptive|disabled")
		}
	case isZhipuEntry(e):
		// GLM's knob is binary (enabled|disabled); map Anthropic / OpenAI-style
		// depth levels onto the nearest valid value so a stale /effort high|low
		// still works. "off" is a retired DeepSeek level meaning "no thinking",
		// which maps to "disabled".
		switch level {
		case "enabled", "disabled":
			return level, nil
		case "off":
			return "disabled", nil
		case "low", "medium", "high", "xhigh", "max":
			return "enabled", nil
		default:
			return "", fmt.Errorf("usage: /effort auto|enabled|disabled")
		}
	case isLongCatEntry(e):
		// LongCat's knob is binary (enabled|disabled); depth-like aliases mean
		// thinking on, while the legacy off spellings disable it.
		switch level {
		case "enabled", "disabled":
			return level, nil
		case "off":
			return "disabled", nil
		case "low", "medium", "high", "xhigh", "max":
			return "enabled", nil
		default:
			return "", fmt.Errorf("usage: /effort auto|enabled|disabled")
		}
	case isOllamaCloudEntry(e):
		switch level {
		case "none", "disabled", "off":
			return "none", nil
		case "low", "medium", "high", "max":
			return level, nil
		case "xhigh":
			return "max", nil
		default:
			return "", fmt.Errorf("usage: /effort auto|none|low|medium|high|max")
		}
	case e != nil && e.Kind == "anthropic":
		switch level {
		case "low", "medium", "high", "xhigh", "max":
			return level, nil
		default:
			return "", fmt.Errorf("usage: /effort auto|low|medium|high|xhigh|max")
		}
	default:
		return "", effortNotConfigurableError(e)
	}
}

// EffortDisplay returns the selected /effort level, using "auto" for provider
// default.
func EffortDisplay(e *ProviderEntry) string {
	if e == nil || strings.TrimSpace(e.Effort) == "" {
		return "auto"
	}
	return normalizeEffortLevel(e.Effort)
}

// EffectiveEffort resolves the provider-visible effort value. Explicit
// ProviderEntry.Effort wins; otherwise a configured SupportedEfforts list makes
// DefaultEffort (or the first supported level) the runtime default. Empty means
// provider default / omit the provider-specific effort field.
func EffectiveEffort(e *ProviderEntry) string {
	if e == nil {
		return ""
	}
	if effort := normalizeStoredEffort(e.Effort); effort != "" {
		return effort
	}
	supported := normalizedSupportedEfforts(e)
	if len(supported) == 0 {
		return ""
	}
	def := normalizeEffortLevel(e.DefaultEffort)
	if def == "" || !containsString(supported, def) {
		return supported[0]
	}
	return def
}

func normalizeEffortConfig(c *Config) {
	if c == nil {
		return
	}
	for i := range c.Providers {
		normalizeProviderEffortFields(&c.Providers[i])
	}
}

func normalizeProviderEffortFields(e *ProviderEntry) {
	if e == nil {
		return
	}
	e.Headers = normalizedProviderHeaders(e.Headers)
	e.Effort = normalizeStoredEffort(e.Effort)
	e.ReasoningProtocol = normalizeReasoningProtocol(e.ReasoningProtocol)
	e.DefaultEffort = normalizeEffortLevel(e.DefaultEffort)
	e.SupportedEfforts = normalizedSupportedEfforts(e)
	e.ModelOverrides = normalizedModelOverrides(e.ModelOverrides)
}

func normalizeStoredEffort(raw string) string {
	level := normalizeEffortLevel(raw)
	if level == "auto" || level == "off" {
		return ""
	}
	return level
}

// ReasoningProtocolForEntry resolves the provider request shape for reasoning
// controls. Explicit config wins, then the model capability registry, then legacy
// endpoint heuristics.
func ReasoningProtocolForEntry(e *ProviderEntry) string {
	if explicit := explicitReasoningProtocol(e); explicit != "" {
		return explicit
	}
	if cap, ok := resolvedModelReasoningCapability(e); ok {
		return cap.Protocol
	}
	if isDeepSeekEntry(e) {
		return ReasoningProtocolDeepSeek
	}
	return ""
}

func explicitReasoningProtocol(e *ProviderEntry) string {
	if e == nil {
		return ""
	}
	protocol := normalizeReasoningProtocol(e.ReasoningProtocol)
	if protocol == ReasoningProtocolAuto {
		return ""
	}
	return protocol
}

func normalizeReasoningProtocol(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", ReasoningProtocolAuto:
		return ""
	case ReasoningProtocolDeepSeek, ReasoningProtocolOpenAI, ReasoningProtocolNone:
		return strings.ToLower(strings.TrimSpace(raw))
	default:
		return ""
	}
}

// isDeepSeekEntry reports whether the entry points at DeepSeek's API. The
// actual host matching lives in provider/openai so the openai package and
// the config layer stay in lockstep when new gateways are added.
func isDeepSeekEntry(e *ProviderEntry) bool {
	return e != nil && e.Kind == "openai" && openai.IsDeepSeek(e.BaseURL)
}

// isMiniMaxEntry reports whether the entry points at MiniMax's OpenAI-compatible
// endpoint. See openai.IsMiniMax for the host-matching rule; the entry-wrapper
// just gates on the openai kind.
func isMiniMaxEntry(e *ProviderEntry) bool {
	return e != nil && e.Kind == "openai" && openai.IsMiniMax(e.BaseURL)
}

// isZhipuEntry reports whether the entry points at Zhipu's OpenAI-compatible
// endpoint for GLM models. See openai.IsZhipu for the host-matching rule; the
// entry-wrapper just gates on the openai kind.
func isZhipuEntry(e *ProviderEntry) bool {
	return e != nil && e.Kind == "openai" && openai.IsZhipu(e.BaseURL)
}

// isLongCatEntry reports whether the entry points at LongCat's OpenAI-compatible
// endpoint. See openai.IsLongCat for the host-matching rule.
func isLongCatEntry(e *ProviderEntry) bool {
	return e != nil && e.Kind == "openai" && openai.IsLongCat(e.BaseURL)
}

// isOllamaCloudEntry reports whether the entry points at hosted Ollama Cloud,
// whose OpenAI-compatible endpoint accepts reasoning_effort=max. Local Ollama
// endpoints intentionally do not match.
func isOllamaCloudEntry(e *ProviderEntry) bool {
	return e != nil && e.Kind == "openai" && openai.IsOllamaCloud(e.BaseURL)
}

func resolvedModelReasoningCapability(e *ProviderEntry) (modelReasoningCapability, bool) {
	if e == nil || e.Kind != "openai" {
		return modelReasoningCapability{}, false
	}
	cap, ok := modelReasoningCapabilities[strings.ToLower(strings.TrimSpace(e.Model))]
	return cap, ok
}

func effortCapabilityFromModel(cap modelReasoningCapability) EffortCapability {
	levels := make([]string, 0, len(cap.Levels)+1)
	levels = append(levels, "auto")
	levels = append(levels, cap.Levels...)
	def := normalizeEffortLevel(cap.Default)
	if def == "" || !containsString(cap.Levels, def) {
		def = "auto"
	}
	return EffortCapability{Supported: true, Levels: levels, Default: def}
}

func deepSeekEffortCapability() EffortCapability {
	return EffortCapability{Supported: true, Levels: []string{"auto", "disabled", "high", "max"}, Default: "high"}
}

func openAIEffortCapability() EffortCapability {
	return EffortCapability{Supported: true, Levels: []string{"auto", "low", "medium", "high"}, Default: "auto"}
}

func effortNotConfigurableError(e *ProviderEntry) error {
	name := ""
	if e != nil {
		name = e.Name
	}
	if name == "" {
		name = "this model"
	}
	return fmt.Errorf("effort is not configurable for %s", name)
}

func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

func normalizeEffortLevel(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func normalizedSupportedEfforts(e *ProviderEntry) []string {
	if e == nil || len(e.SupportedEfforts) == 0 {
		return nil
	}
	return normalizedEffortLevels(e.SupportedEfforts)
}

func normalizedEffortLevels(levels []string) []string {
	if len(levels) == 0 {
		return nil
	}
	out := make([]string, 0, len(levels))
	seen := map[string]bool{}
	for _, raw := range levels {
		level := normalizeEffortLevel(raw)
		if level == "" || level == "auto" || seen[level] {
			continue
		}
		seen[level] = true
		out = append(out, level)
	}
	return out
}

func normalizedProviderHeaders(headers map[string]string) map[string]string {
	if len(headers) == 0 {
		return nil
	}
	out := make(map[string]string, len(headers))
	for rawName, rawValue := range headers {
		name := strings.TrimSpace(rawName)
		value := strings.TrimSpace(rawValue)
		if name == "" || value == "" {
			continue
		}
		out[name] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func normalizedModelOverrides(overrides map[string]ProviderModelOverride) map[string]ProviderModelOverride {
	if len(overrides) == 0 {
		return nil
	}
	out := make(map[string]ProviderModelOverride, len(overrides))
	for rawModel, ov := range overrides {
		model := strings.TrimSpace(rawModel)
		if model == "" {
			continue
		}
		ov.ReasoningProtocol = normalizeReasoningProtocol(ov.ReasoningProtocol)
		ov.SupportedEfforts = normalizedEffortLevels(ov.SupportedEfforts)
		ov.DefaultEffort = normalizeEffortLevel(ov.DefaultEffort)
		if ov.DefaultEffort != "" && !containsString(ov.SupportedEfforts, ov.DefaultEffort) {
			ov.DefaultEffort = ""
		}
		if ov.ReasoningProtocol == "" && len(ov.SupportedEfforts) == 0 && ov.DefaultEffort == "" && ov.Vision == nil {
			continue
		}
		out[model] = ov
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

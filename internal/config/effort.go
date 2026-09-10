package config

import (
	"fmt"
	"slices"
	"strings"

	"reasonix/internal/provider"
	_ "reasonix/internal/provider/anthropic"
	"reasonix/internal/provider/openai"
	_ "reasonix/internal/provider/responses"
)

const (
	ReasoningProtocolAuto     = "auto"
	ReasoningProtocolDeepSeek = "deepseek"
	ReasoningProtocolGLM      = "glm"
	ReasoningProtocolKimiK3   = "kimi-k3"
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

type modelReasoningCapability struct{ Protocol string }

var modelReasoningCapabilities = map[string]modelReasoningCapability{
	"deepseek-v4-flash": {Protocol: ReasoningProtocolDeepSeek},
	"deepseek-v4-pro":   {Protocol: ReasoningProtocolDeepSeek},
}

// EffortCapabilityForEntry returns the user-facing /effort levels for a resolved
// provider entry. Provider implementations still decide how a stored effort is
// serialized into requests.
func ReasoningCapabilityForEntry(e *ProviderEntry) provider.ReasoningCapability {
	if e == nil {
		return provider.ReasoningOptions("")
	}
	// Resolver-backed entries carry the remote adapter declaration, not a local kind.
	if e.Kind == "" {
		return provider.ReasoningOptions(e.DefaultEffort, e.SupportedEfforts...)
	}
	cfg := provider.Config{Name: e.Name, BaseURL: e.BaseURL, Model: e.Model, Extra: map[string]any{
		"thinking": e.Thinking, "reasoning_protocol": ReasoningProtocolForEntry(e),
		"request_url": e.RequestURL, "chat_url": e.ChatURL,
		"supported_efforts": normalizedSupportedEfforts(e), "default_effort": normalizeEffortLevel(e.DefaultEffort),
	}}
	return provider.ReasoningForConfig(e.Kind, cfg)
}
func EffortCapabilityForEntry(e *ProviderEntry) EffortCapability {
	cap := ReasoningCapabilityForEntry(e)
	if len(cap.Options) == 0 {
		return EffortCapability{}
	}
	def := cap.Default
	if def == "" {
		def = "auto"
	}
	return EffortCapability{Supported: true, Levels: append([]string{"auto"}, cap.IDs()...), Default: def}
}

// NormalizeEffort maps a user-supplied /effort level into the value stored in
// config. Empty means auto/provider default.
func NormalizeEffort(e *ProviderEntry, raw string) (string, error) {
	// auto is the historical spelling for inheriting the provider default.
	if raw == "auto" {
		return "", nil
	}
	if raw == "" {
		return "", fmt.Errorf("usage: /effort auto|<level>")
	}
	cap := ReasoningCapabilityForEntry(e)
	model := ""
	if e != nil {
		model = e.Model
	}
	if err := cap.Validate(model, raw); err != nil {
		return "", err
	}
	return raw, nil
}

// EffortDisplay returns the selected /effort level, using "auto" for provider
// default.
func EffortDisplay(e *ProviderEntry) string {
	if e == nil || strings.TrimSpace(e.Effort) == "" {
		return "auto"
	}
	effort := normalizeEffortLevel(e.Effort)

	return effort
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
		return migrateStoredDeepSeekEffort(e, effort)
	}
	if explicitReasoningProtocol(e) == ReasoningProtocolKimiK3 {
		return ""
	}
	supported := normalizedSupportedEfforts(e)
	if len(supported) == 0 {
		return ""
	}
	def := normalizeEffortLevel(e.DefaultEffort)
	if def == "" {
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
	if e != nil {
		if contract, ok := provider.LookupOpenCodeGoContract(e.Kind, e.BaseURL, e.RequestURL, e.ChatURL, e.Model); ok {
			return contract.ReasoningProtocol
		}
	}
	if cap, ok := resolvedModelReasoningCapability(e); ok {
		return cap.Protocol
	}
	if isTokenRhythmGLMEntry(e) {
		return ReasoningProtocolGLM
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
	case ReasoningProtocolDeepSeek, ReasoningProtocolGLM, ReasoningProtocolKimiK3, ReasoningProtocolOpenAI, ReasoningProtocolNone:
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

// isTokenRhythmGLMEntry upgrades older Token Rhythm configurations that predate
// per-model protocol overrides. Keep the rule scoped to the gateway and exact
// official model IDs so unrelated mixed-model providers retain their existing
// request shape.
func isTokenRhythmGLMEntry(e *ProviderEntry) bool {
	if e == nil || e.Kind != "openai" || !openai.IsTokenRhythm(e.BaseURL) {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(e.Model)) {
	case "glm-5", "glm-5.1", "glm-5.2":
		return true
	default:
		return false
	}
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

// isMimoEntry reports whether the entry points at Xiaomi MiMo's Responses API
// (api.xiaomimimo.com). Host matching mirrors provider/responses.DetectVendor
// but lives in the config layer to avoid an import cycle (control → config,
// not control → provider). Host-based exact/suffix matching (not full-URL
// substring) so unrelated or attacker-controlled URLs can't enable MiMo
// effort. The kind check is intentionally absent: MiMo is served through both
// kind="responses" and kind="openai" presets.
func isMimoEntry(e *ProviderEntry) bool {
	if e == nil {
		return false
	}
	host := officialProviderHost(e.BaseURL)
	return host == "api.xiaomimimo.com" || strings.HasSuffix(host, ".xiaomimimo.com")
}

func resolvedModelReasoningCapability(e *ProviderEntry) (modelReasoningCapability, bool) {
	if e == nil || e.Kind != "openai" {
		return modelReasoningCapability{}, false
	}
	return modelReasoningCapabilityForEntry(e)
}

func modelReasoningCapabilityForEntry(e *ProviderEntry) (modelReasoningCapability, bool) {
	if e == nil {
		return modelReasoningCapability{}, false
	}
	cap, ok := modelReasoningCapabilities[strings.ToLower(strings.TrimSpace(e.Model))]
	return cap, ok
}

func containsString(haystack []string, needle string) bool {
	return slices.Contains(haystack, needle)
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
		if ov.ContextWindow < 0 {
			ov.ContextWindow = 0
		}

		if ov.ReasoningProtocol == "" && len(ov.SupportedEfforts) == 0 && ov.DefaultEffort == "" && ov.Vision == nil && ov.ContextWindow == 0 {
			continue
		}
		out[model] = ov
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

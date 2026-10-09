package config

import (
	"errors"
	"fmt"
	"reasonix/internal/contract/provider"
	"slices"
	"strings"
)

const (
	ReasoningProtocolAuto      = "auto"
	ReasoningProtocolAnthropic = "anthropic"
	ReasoningProtocolDeepSeek  = "deepseek"
	ReasoningProtocolGLM       = "glm"
	ReasoningProtocolKimiK3    = "kimi-k3"
	ReasoningProtocolOpenAI    = "openai"
	ReasoningProtocolNone      = "none"
)

// EffortCapability describes the abstract effort levels a provider/model can set
// through the /effort command.
type EffortCapability struct {
	Supported bool
	Levels    []string
	Default   string
}

// effortCapabilityForProtocol is the one place a reasoning protocol names its
// levels. Declaring the protocol and having it inferred used to read from two
// separate switches, and the inferred one was missing MiMo — so an endpoint
// that accepts none|low|medium|high reported having no levels at all.
func effortCapabilityForProtocol(e *ProviderEntry, protocol string) (EffortCapability, bool) {
	switch protocol {
	case ReasoningProtocolDeepSeek:
		if cap, ok := resolvedModelEffortLadder(e); ok && cap.Protocol == ReasoningProtocolDeepSeek {
			return effortCapabilityFromModel(cap), true
		}
		return deepSeekEffortCapability(e), true
	case ReasoningProtocolGLM:
		return zhipuEffortCapability(e), true
	case ReasoningProtocolKimiK3:
		return kimiK3EffortCapability(), true
	case ReasoningProtocolAnthropic:
		return anthropicEffortCapability(), true
	case ReasoningProtocolOpenAI:
		if isMimoEntry(e) {
			return mimoEffortCapability(), true
		}
		return openAIEffortCapability(), true
	}
	return EffortCapability{}, false
}

// EffortCapabilityForEntry returns the user-facing /effort levels for a resolved
// provider entry. Provider implementations still decide how a stored effort is
// serialized into requests.
func EffortCapabilityForEntry(e *ProviderEntry) EffortCapability {
	explicitProtocol := explicitReasoningProtocol(e)
	if explicitProtocol == ReasoningProtocolNone {
		return EffortCapability{}
	}
	// Kimi K3 is a complete wire contract, including its fixed effort
	// vocabulary. Keep any persisted supported_efforts metadata dormant while
	// the protocol is selected so switching protocols can restore it later.
	if explicitProtocol == ReasoningProtocolKimiK3 {
		return kimiK3EffortCapability()
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
	if cap, ok := effortCapabilityForProtocol(e, explicitProtocol); ok {
		return cap
	}
	if cap, ok := resolvedModelEffortLadder(e); ok {
		return effortCapabilityFromModel(cap)
	}
	if cap, ok := effortCapabilityForProtocol(e, ReasoningProtocolForEntry(e)); ok {
		return cap
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
		return zhipuEffortCapability(e)
	case isLongCatEntry(e):
		// LongCat exposes the same binary thinking vocabulary on its
		// OpenAI-compatible endpoint and documents no reasoning_effort depth scale.
		return binaryThinkingEffortCapability("enabled")
	case isOllamaCloudEntry(e):
		// Ollama Cloud accepts top-level reasoning_effort values low|medium|
		// high|max. "none" means omit the field so the hosted model runs without
		// thinking. Leave auto as the default so existing traffic stays provider-
		// default until the user chooses an effort explicitly.
		return EffortCapability{Supported: true, Levels: []string{"auto", "none", "low", "medium", "high", "max"}, Default: "auto"}
	case e != nil && e.Kind == "anthropic":
		// An Anthropic-compatible gateway that declared nothing keeps the
		// binary toggle; depth takes a declaration (isAnthropicDepthEntry).
		return binaryThinkingEffortCapability("auto")
	default:
		return EffortCapability{}
	}
}

// NormalizeInheritedEffort accepts an inherited effort only when the entry's own
// contract can use it, never through NormalizeEffort's cross-provider mappings.
// A false result means the caller omits the override and the model's default applies.
func NormalizeInheritedEffort(e *ProviderEntry, raw string) (string, bool) {
	level := normalizeEffortLevel(raw)
	if level == "" {
		return "", false
	}
	if level == "auto" {
		return "", true
	}

	cap := EffortCapabilityForEntry(e)
	if !cap.Supported {
		return "", false
	}
	if containsString(cap.Levels, level) {
		return level, true
	}

	// An explicit supported_efforts list is the whole vocabulary; no aliases.
	if len(normalizedSupportedEfforts(e)) > 0 {
		return "", false
	}

	// Model-declared aliases only; a relay's synthesized ladder aliases are remaps.
	if modelCap, ok := resolvedModelReasoningCapability(e); ok {
		explicit := explicitReasoningProtocol(e)
		if explicit == "" || explicit == modelCap.Protocol {
			if normalized, ok := modelCap.Aliases[level]; ok && containsString(cap.Levels, normalized) {
				return normalized, true
			}
		}
	}

	// DeepSeek keeps legacy spellings (off, medium, xhigh); max -> high is a remap.
	if ReasoningProtocolForEntry(e) == ReasoningProtocolDeepSeek {
		if normalized, err := normalizeDeepSeekReasoningEffort(e, level); err == nil && normalized != level && containsString(cap.Levels, normalized) {
			if level == "max" {
				return "", false
			}
			return normalized, true
		}
	}

	return "", false
}

// ErrEffortUnsupported marks a level the entry's own reasoning contract cannot
// carry, so callers branch on errors.Is rather than on the message.
var ErrEffortUnsupported = errors.New("effort level not supported by the model")

type effortUnsupportedError struct{ err error }

func (e effortUnsupportedError) Error() string        { return e.err.Error() }
func (e effortUnsupportedError) Unwrap() error        { return e.err }
func (e effortUnsupportedError) Is(target error) bool { return target == ErrEffortUnsupported }

// NormalizeEffort maps a user-supplied /effort level into the value stored in
// config. Empty means auto/provider default. A refusal satisfies
// errors.Is(err, ErrEffortUnsupported).
func NormalizeEffort(e *ProviderEntry, raw string) (string, error) {
	out, err := normalizeEffort(e, raw)
	if err != nil {
		return "", effortUnsupportedError{err}
	}
	return out, nil
}

func normalizeEffort(e *ProviderEntry, raw string) (string, error) {
	level := normalizeEffortLevel(raw)
	if level == "" {
		return "", fmt.Errorf("usage: /effort auto|<level>")
	}
	if level == "auto" {
		return "", nil
	}
	explicitProtocol := explicitReasoningProtocol(e)
	if explicitProtocol == ReasoningProtocolNone {
		return "", effortNotConfigurableError(e)
	}
	if explicitProtocol == ReasoningProtocolKimiK3 {
		return normalizeKimiK3ReasoningEffort(level)
	}
	supported := normalizedSupportedEfforts(e)
	if len(supported) > 0 {
		if containsString(supported, level) {
			return level, nil
		}
		return "", fmt.Errorf("usage: /effort auto|%s", strings.Join(supported, "|"))
	}
	// V4 Flash 0731 added a real low depth. Keep this model-scoped: Pro and
	// generic DeepSeek-compatible endpoints still normalize low to high unless
	// they explicitly advertise a different supported_efforts list.
	if cap, ok := resolvedModelEffortLadder(e); ok {
		explicit := explicitReasoningProtocol(e)
		if explicit == "" || explicit == cap.Protocol {
			if containsString(cap.Levels, level) {
				return level, nil
			}
			if normalized, ok := cap.Aliases[level]; ok && containsString(cap.Levels, normalized) {
				return normalized, nil
			}
		}
	}
	switch ReasoningProtocolForEntry(e) {
	case ReasoningProtocolDeepSeek:
		return normalizeDeepSeekReasoningEffort(e, level)
	case ReasoningProtocolOpenAI:
		return normalizeOpenAIReasoningEffort(e, level)
	case ReasoningProtocolKimiK3:
		return normalizeKimiK3ReasoningEffort(level)
	case ReasoningProtocolGLM:
		return normalizeZhipuEffort(e, level)
	case ReasoningProtocolAnthropic:
		return normalizeAnthropicEffort(level)
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
		return normalizeZhipuEffort(e, level)
	case isLongCatEntry(e):
		// LongCat's knob is binary (enabled|disabled); depth-like aliases mean
		// thinking on, while the legacy off spellings disable it.
		return normalizeBinaryThinkingEffort(level)
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
		return normalizeBinaryThinkingEffort(level)
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
	effort := normalizeEffortLevel(e.Effort)
	effort = zhipuLegacyStoredEffort(e, effort)
	if !effortInContract(e, effort) {
		return "auto"
	}
	return effort
}

// EffectiveEffort resolves the provider-visible effort value. Explicit
// ProviderEntry.Effort wins; otherwise a configured SupportedEfforts list makes
// DefaultEffort (or the first supported level) the runtime default. A stored
// level outside the resolved menu reads as auto, as EffortDisplay shows it.
// Empty means provider default / omit the provider-specific effort field.
func EffectiveEffort(e *ProviderEntry) string {
	if e == nil {
		return ""
	}
	if effort := zhipuLegacyStoredEffort(e, normalizeStoredEffort(e.Effort)); effort != "" && effortInContract(e, effort) {
		return effort
	}
	if explicitReasoningProtocol(e) == ReasoningProtocolKimiK3 {
		return ""
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
	if isAnthropicDepthEntry(e) {
		return ReasoningProtocolAnthropic
	}
	if isTokenRhythmGLMEntry(e) {
		return ReasoningProtocolGLM
	}
	if isDeepSeekEntry(e) {
		return ReasoningProtocolDeepSeek
	}
	if isMimoEntry(e) {
		return ReasoningProtocolOpenAI
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

// StoredReasoningProtocol validates a declared protocol and returns what to
// store for it. Auto stores as empty — no declaration is what leaves the model
// registry and endpoint heuristics in charge. An unrecognised value is refused
// rather than normalized away: a typo that quietly means "auto" is a setting
// that does nothing and reads as one that failed.
func StoredReasoningProtocol(raw string) (string, bool) {
	value := strings.ToLower(strings.TrimSpace(raw))
	switch value {
	case "", ReasoningProtocolAuto:
		return "", true
	case ReasoningProtocolAnthropic, ReasoningProtocolDeepSeek, ReasoningProtocolGLM,
		ReasoningProtocolKimiK3, ReasoningProtocolOpenAI, ReasoningProtocolNone:
		return value, true
	default:
		return "", false
	}
}

// StoredEffortLevels is what a declared effort vocabulary stores as: trimmed,
// lower-cased, deduplicated, and without auto, which every ladder carries
// implicitly. An empty result clears the declaration.
func StoredEffortLevels(levels []string) []string {
	return normalizedEffortLevels(levels)
}

func normalizeReasoningProtocol(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", ReasoningProtocolAuto:
		return ""
	case ReasoningProtocolAnthropic, ReasoningProtocolDeepSeek, ReasoningProtocolGLM, ReasoningProtocolKimiK3, ReasoningProtocolOpenAI, ReasoningProtocolNone:
		return strings.ToLower(strings.TrimSpace(raw))
	default:
		return ""
	}
}

func kimiK3EffortCapability() EffortCapability {
	return EffortCapability{Supported: true, Levels: []string{"auto", "low", "high", "max"}, Default: "max"}
}

// effortInContract reports whether a stored level is one the endpoint's
// resolved menu still exposes. A level outside it goes dormant instead of
// erroring: a protocol switch, or an effort persisted before the endpoint's
// vocabulary was known, must not resurrect a value the endpoint would reject.
// An unknown vocabulary keeps the value — the provider layer validates it.
func effortInContract(e *ProviderEntry, level string) bool {
	cap := EffortCapabilityForEntry(e)
	if !cap.Supported {
		return true
	}
	return containsString(cap.Levels, level)
}

// isDeepSeekEntry reports whether the entry points at DeepSeek's API. The
// actual host matching lives in provider/openai so the openai package and
// the config layer stay in lockstep when new gateways are added.
func isDeepSeekEntry(e *ProviderEntry) bool {
	return e != nil && e.Kind == "openai" && provider.IsDeepSeekEndpoint(e.BaseURL)
}

// isMiniMaxEntry reports whether the entry points at MiniMax's OpenAI-compatible
// endpoint. See provider.IsMiniMaxEndpoint for the host-matching rule; the entry-wrapper
// just gates on the openai kind.
func isMiniMaxEntry(e *ProviderEntry) bool {
	return e != nil && e.Kind == "openai" && provider.IsMiniMaxEndpoint(e.BaseURL)
}

// isZhipuEntry reports whether the entry points at Zhipu's OpenAI-compatible
// endpoint for GLM models. See provider.IsZhipuEndpoint for the host-matching rule; the
// entry-wrapper just gates on the openai kind.
func isZhipuEntry(e *ProviderEntry) bool {
	return e != nil && e.Kind == "openai" && provider.IsZhipuEndpoint(e.BaseURL)
}

// isTokenRhythmGLMEntry upgrades older Token Rhythm configurations that predate
// per-model protocol overrides. Keep the rule scoped to the gateway and exact
// official model IDs so unrelated mixed-model providers retain their existing
// request shape.
func isTokenRhythmGLMEntry(e *ProviderEntry) bool {
	if e == nil || e.Kind != "openai" || !provider.IsTokenRhythmEndpoint(e.BaseURL) {
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
	return e != nil && e.Kind == "openai" && provider.IsLongCatEndpoint(e.BaseURL)
}

// isOllamaCloudEntry reports whether the entry points at hosted Ollama Cloud,
// whose OpenAI-compatible endpoint accepts reasoning_effort=max. Local Ollama
// endpoints intentionally do not match.
func isOllamaCloudEntry(e *ProviderEntry) bool {
	return e != nil && e.Kind == "openai" && provider.IsOllamaCloudEndpoint(e.BaseURL)
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

// mimoEffortCapability mirrors MiMo's documented binary thinking knob: "none"
// disables reasoning, every other legal value enables it (no real depth
// difference server-side). The vendor accepts the OpenAI depth vocabulary.
func mimoEffortCapability() EffortCapability {
	return EffortCapability{Supported: true, Levels: []string{"auto", "none", "low", "medium", "high"}, Default: "auto"}
}

// deepSeekEffortCapability is the ladder for a DeepSeek-protocol endpoint with
// no model-table entry. "max" is the vendor's own extension, so only the
// vendor's endpoint is offered it: a relay that never took it answers 400 for
// as long as the setting stands, and one that does can say so with
// supported_efforts.
func deepSeekEffortCapability(e *ProviderEntry) EffortCapability {
	levels := []string{"auto", "disabled", "high", "max"}
	if !servedByVendor(e, deepSeekVendor) {
		levels = levels[:len(levels)-1]
	}
	return EffortCapability{Supported: true, Levels: levels, Default: "high"}
}

func openAIEffortCapability() EffortCapability {
	return EffortCapability{Supported: true, Levels: []string{"auto", "low", "medium", "high"}, Default: "auto"}
}

// binaryThinkingEffortCapability is the menu for an endpoint whose only
// reasoning control is on/off: Zhipu GLM, LongCat, and undeclared
// Anthropic-compatible gateways. def is what "auto" resolves to.
func binaryThinkingEffortCapability(def string) EffortCapability {
	return EffortCapability{Supported: true, Levels: []string{"auto", "enabled", "disabled"}, Default: def}
}

// zhipuEffortCapability is the /effort menu for a Zhipu GLM entry. The depth
// models (GLM-5.2, GLM-5.3, GLM-5.3-Flash) take their levels from the one
// contract table in the provider package; every other GLM keeps the binary
// thinking knob. Zhipu documents the levels per model at
// https://docs.z.ai/guides/overview/concept-param.
func zhipuEffortCapability(e *ProviderEntry) EffortCapability {
	contract, ok := zhipuDepthContract(e)
	if !ok {
		return binaryThinkingEffortCapability("enabled")
	}
	levels := make([]string, 0, len(contract.Levels)+1)
	levels = append(levels, "auto")
	levels = append(levels, contract.Levels...)
	return EffortCapability{Supported: true, Levels: levels, Default: contract.Default}
}

// zhipuDepthContract resolves the documented depth contract for the entry, or
// false when the entry is not a Zhipu GLM depth model.
func zhipuDepthContract(e *ProviderEntry) (provider.ZhipuEffort, bool) {
	if !isZhipuEntry(e) {
		return provider.ZhipuEffort{}, false
	}
	return provider.ZhipuEffortContract(e.Model)
}

// EffortForcesThinking reports whether the entry's model always runs thinking,
// so even its lowest level reasons (and is billed). True for GLM-5.3 and
// GLM-5.3-Flash, which cannot disable thinking; a saved `disabled` choice lands
// on their cheapest level instead. The /effort menu says so where it offers the
// level.
func EffortForcesThinking(e *ProviderEntry) bool {
	contract, ok := zhipuDepthContract(e)
	return ok && contract.ForcesThinking()
}

// zhipuLegacyStoredEffort translates a choice saved under the old binary knob
// onto the depth contract. `enabled` becomes the model's documented default;
// `disabled` becomes the contract's DisabledTo — which is `none` where thinking
// can be switched off, and `low` where it cannot (GLM-5.3).
func zhipuLegacyStoredEffort(e *ProviderEntry, effort string) string {
	contract, ok := zhipuDepthContract(e)
	if !ok {
		return effort
	}
	switch effort {
	case "enabled":
		return contract.Default
	case "disabled":
		return contract.DisabledTo
	}
	return effort
}

func normalizeZhipuEffort(e *ProviderEntry, level string) (string, error) {
	cap := zhipuEffortCapability(e)
	if containsString(cap.Levels, level) {
		return level, nil
	}
	if contract, ok := zhipuDepthContract(e); ok {
		switch level {
		case "enabled":
			return contract.Default, nil
		case "disabled", "off":
			return contract.DisabledTo, nil
		}
		return "", fmt.Errorf("usage: /effort %s", strings.Join(cap.Levels, "|"))
	}
	if cap.Default == "enabled" {
		return normalizeBinaryThinkingEffort(level)
	}
	return "", fmt.Errorf("usage: /effort %s", strings.Join(cap.Levels, "|"))
}

// normalizeBinaryThinkingEffort maps depth vocabularies onto the binary knob so
// a level carried over from another provider still means something: any depth
// is thinking on, the retired off spellings are thinking off.
func normalizeBinaryThinkingEffort(level string) (string, error) {
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
		if ov.DefaultEffort != "" && !containsString(ov.SupportedEfforts, ov.DefaultEffort) {
			ov.DefaultEffort = ""
		}
		if modelOverrideEmpty(ov) {
			continue
		}
		out[model] = ov
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

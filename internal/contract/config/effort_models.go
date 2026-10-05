package config

import (
	"slices"
	"strings"
)

// vendorEndpoint is the set of hosts a vendor's own ladder was established
// against. Host-only exact or suffix matching: a full-URL substring would let
// an unrelated URL enable a vendor's extensions.
type vendorEndpoint struct {
	hosts    []string
	suffixes []string
}

func (v vendorEndpoint) serves(host string) bool {
	if host == "" {
		return false
	}
	if slices.Contains(v.hosts, host) {
		return true
	}
	for _, suffix := range v.suffixes {
		if strings.HasSuffix(host, suffix) {
			return true
		}
	}
	return false
}

var (
	deepSeekVendor = vendorEndpoint{hosts: []string{"api.deepseek.com"}}
	openAIVendor   = vendorEndpoint{hosts: []string{"api.openai.com"}}
	// dashScopeVendor is the Model Studio OpenAI-compatible endpoint: the legacy
	// regional hosts and the per-workspace <id>.<region>.maas.aliyuncs.com ones.
	dashScopeVendor = vendorEndpoint{
		hosts:    []string{"dashscope.aliyuncs.com", "dashscope-intl.aliyuncs.com"},
		suffixes: []string{".maas.aliyuncs.com"},
	}
)

// Provider kinds a model ladder can be declared for. Chat Completions and
// Responses carry the same OpenAI vocabulary, but a model may be usable on one.
const (
	kindOpenAI    = "openai"
	kindResponses = "responses"
)

type modelReasoningCapability struct {
	Protocol string
	// Levels belong to who serves the model: only Vendor inherits all of them.
	Levels  []string
	Default string
	Aliases map[string]string
	Vendor  vendorEndpoint
	// Kinds lists the wires the ladder was declared for; nil means openai only.
	Kinds []string
	// ContextWindow is a fact about the model; zero means none is established.
	ContextWindow int
	// Modes are the optional run modes the model takes; see RequestModes.
	Modes []modelModeDecl
}

// modelReasoningCapabilities is keyed by exact model id, so a gateway serving a
// model under the same id inherits its declaration. Every ladder cites the
// vendor's own statement of which levels the model accepts.
var modelReasoningCapabilities = map[string]modelReasoningCapability{
	DeepSeekFlashModel: deepSeekFlashCapability(),
	"deepseek-v4-pro": {
		Protocol:      ReasoningProtocolDeepSeek,
		Vendor:        deepSeekVendor,
		Levels:        []string{"disabled", "high", "max"},
		Default:       "high",
		ContextWindow: 1_000_000,
	},
	// Retired flash ids are the same model and carry the same ladder.
	"deepseek-v4-flash":            deepSeekFlashCapability(),
	"deepseek-v4-flash-vision-exp": deepSeekFlashCapability(),
	// developers.openai.com/api/docs/models/gpt-5.6-sol; "gpt-5.6" routes to sol.
	"gpt-5.6":       gpt56Capability(),
	"gpt-5.6-luna":  gpt56Capability(),
	"gpt-5.6-sol":   gpt56Capability(),
	"gpt-5.6-terra": gpt56Capability(),
	"gpt-6-sol":     gpt6Capability([]string{"none", "low", "medium", "high", "xhigh", "max"}, "medium"),
	"gpt-6-luna":    gpt6Capability([]string{"none", "low", "medium", "high", "xhigh", "max"}, "medium"),
	// GPT-6 Astra answers 400 for "none"; its default is not documented.
	"gpt-6-astra":       gpt6Capability([]string{"low", "medium", "high", "xhigh", "max"}, ""),
	"qwen3.8-max":       qwen38Capability(),
	"qwen3.8-max-0902":  qwen38Capability(),
	"qwen3.8-flash":     qwen38Capability(),
	"qwen3.8-2.4t-a95b": qwen38Capability(),
	"qwen3.8-27b":       qwen38Capability(),
}

// deepSeekFlashCapability is the flash ladder, measured 2026-09-13: the endpoint
// names low|medium|high|xhigh|ultra|max, rejects minimal and none, and stops
// thinking only for thinking.type=disabled, so "disabled" is a level of ours.
func deepSeekFlashCapability() modelReasoningCapability {
	return modelReasoningCapability{
		Protocol:      ReasoningProtocolDeepSeek,
		Vendor:        deepSeekVendor,
		Levels:        []string{"disabled", "low", "high", "max"},
		Default:       "high",
		Aliases:       map[string]string{"minimal": "low", "medium": "high", "xhigh": "high", "ultra": "max"},
		ContextWindow: 1_000_000,
	}
}

// gpt56Capability: the endpoint refuses "minimal" by naming the model, though
// the generic API vocabulary carries it. No window: one nobody checked would
// silently move compaction.
func gpt56Capability() modelReasoningCapability {
	return modelReasoningCapability{
		Protocol: ReasoningProtocolOpenAI,
		Levels:   []string{"none", "low", "medium", "high", "xhigh", "max"},
		Default:  "medium",
		Aliases:  map[string]string{"minimal": "low"},
		Vendor:   openAIVendor,
		Kinds:    []string{kindOpenAI, kindResponses},
		Modes:    []modelModeDecl{openAIProMode},
	}
}

// gpt6Capability is Responses-only: on Chat Completions GPT-6 takes function
// calls only at reasoning_effort=none, and every turn here carries tools.
func gpt6Capability(levels []string, def string) modelReasoningCapability {
	return modelReasoningCapability{
		Protocol: ReasoningProtocolOpenAI,
		Levels:   levels,
		Default:  def,
		Aliases:  map[string]string{"minimal": "low"},
		Vendor:   openAIVendor,
		Kinds:    []string{kindResponses},
		Modes:    []modelModeDecl{openAIProMode},
	}
}

// qwen38Capability is Model Studio's Qwen3.8 contract: low|medium|xhigh, with
// none taken as enable_thinking=false and high, max, minimal folded server-side.
func qwen38Capability() modelReasoningCapability {
	return modelReasoningCapability{
		Protocol: ReasoningProtocolOpenAI,
		Levels:   []string{"none", "low", "medium", "xhigh"},
		Default:  "xhigh",
		Aliases:  map[string]string{"minimal": "low", "high": "xhigh", "max": "xhigh"},
		Vendor:   dashScopeVendor,
	}
}

// RequestEffortLevels is the vocabulary a request may carry for this entry: a
// declared supported_efforts list, otherwise the model table's ladder for the
// endpoint in hand. The provider validates against it, so a level the menu
// offers is never one the request builder refuses or clamps.
func RequestEffortLevels(e *ProviderEntry) []string {
	if supported := normalizedSupportedEfforts(e); len(supported) > 0 {
		return supported
	}
	cap, ok := resolvedModelEffortLadder(e)
	if !ok {
		return nil
	}
	if explicit := explicitReasoningProtocol(e); explicit != "" && explicit != cap.Protocol {
		return nil
	}
	return append([]string(nil), cap.Levels...)
}

// resolvedModelEffortLadder is the model table's ladder. The vendor's endpoint
// gets all of it; anyone else serving the same model gets its standard
// vocabulary, because a relay that never took "max" answers 400 for as long
// as the setting stands.
func resolvedModelEffortLadder(e *ProviderEntry) (modelReasoningCapability, bool) {
	cap, ok := resolvedModelReasoningCapability(e)
	if !ok {
		return modelReasoningCapability{}, false
	}
	if servedByVendor(e, cap.Vendor) {
		return cap, true
	}
	return standardLadder(cap), true
}

func servedByVendor(e *ProviderEntry, vendor vendorEndpoint) bool {
	return e != nil && vendor.serves(officialProviderHost(e.BaseURL))
}

// standardLadder drops the levels only the vendor's own endpoint is known to
// take, keeping the API's documented depth vocabulary plus the switches, so the
// control stays on screen for a relay. supported_efforts restores the rest.
func standardLadder(cap modelReasoningCapability) modelReasoningCapability {
	out := cap
	out.Levels = nil
	var dropped []string
	for _, level := range cap.Levels {
		if standardEffortLevel(level) {
			out.Levels = append(out.Levels, level)
			continue
		}
		dropped = append(dropped, level)
	}
	// A dropped level degrades onto the deepest standard one rather than being
	// refused, the way "minimal" already degrades.
	if deepest := deepestStandardLevel(out.Levels); deepest != "" && len(dropped) > 0 {
		aliases := make(map[string]string, len(cap.Aliases)+len(dropped))
		for from, to := range cap.Aliases {
			if containsString(dropped, to) {
				to = deepest
			}
			aliases[from] = to
		}
		for _, level := range dropped {
			aliases[level] = deepest
		}
		out.Aliases = aliases
	}
	if !containsString(out.Levels, out.Default) {
		out.Default = ""
	}
	return out
}

// deepestStandardLevel is the most depth a standard vocabulary can ask for.
func deepestStandardLevel(levels []string) string {
	for _, level := range []string{"high", "medium", "low"} {
		if containsString(levels, level) {
			return level
		}
	}
	return ""
}

// standardEffortLevel reports a level any OpenAI-compatible endpoint is
// expected to take. "max" and "xhigh" are vendor extensions and are the ones an
// unmeasured endpoint rejects.
func standardEffortLevel(level string) bool {
	switch level {
	case "auto", "none", "disabled", "off", "low", "medium", "high":
		return true
	}
	return false
}

func resolvedModelReasoningCapability(e *ProviderEntry) (modelReasoningCapability, bool) {
	if e == nil {
		return modelReasoningCapability{}, false
	}
	cap, ok := modelReasoningCapabilities[strings.ToLower(strings.TrimSpace(e.Model))]
	if !ok {
		return modelReasoningCapability{}, false
	}
	kinds := cap.Kinds
	if kinds == nil {
		kinds = []string{kindOpenAI}
	}
	if !containsString(kinds, e.Kind) {
		return modelReasoningCapability{}, false
	}
	return cap, true
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

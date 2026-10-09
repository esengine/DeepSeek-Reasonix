package provider

import "strings"

// ZhipuEffort is the documented reasoning_effort contract for one GLM depth
// model, taken from Zhipu's Core Parameters reference
// (https://docs.z.ai/guides/overview/concept-param) so the /effort menu and the
// request builder read one list. A model absent from the table has no
// documented depth scale and keeps the binary thinking knob.
type ZhipuEffort struct {
	// Levels are the depth levels the model accepts, in /effort menu order.
	Levels []string
	// Default is the level the API applies when the request omits the field.
	Default string
	// ThinkingOff lists the levels that switch thinking off through
	// thinking.type=disabled. Empty means the model cannot disable thinking.
	ThinkingOff []string
	// DisabledTo is where a stored binary `disabled` choice lands: the level
	// itself when thinking can be switched off, otherwise the cheapest level
	// the model does accept.
	DisabledTo string
}

// zhipuEffortContracts is keyed by exact model id. Zhipu's Core Parameters page
// states the ladder per model, not per host, so a gateway serving the same id
// gets the same contract only when the caller has opted in (see ZhipuDepthModel
// and the openai client, which require the vendor host as well).
var zhipuEffortContracts = map[string]ZhipuEffort{
	// "GLM-5.2 supports max, xhigh, high, medium, low, minimal, none; passing
	// none or minimal will cause the model to skip thinking; low and medium
	// will be mapped to high; xhigh will be mapped to max." Default: max.
	"glm-5.2": {
		Levels:      []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"},
		Default:     "max",
		ThinkingOff: []string{"none", "minimal"},
		DisabledTo:  "none",
	},
	// "The GLM-5.3 and GLM-5.3-FLASH models only support max, high, low", they
	// "always operate with reasoning enabled", and disabling "is no longer
	// supported" — so a stored `disabled` lands on the cheapest level, low.
	"glm-5.3":       {Levels: []string{"low", "high", "max"}, Default: "max", DisabledTo: "low"},
	"glm-5.3-flash": {Levels: []string{"low", "high", "max"}, Default: "max", DisabledTo: "low"},
}

// ZhipuEffortContract returns the documented depth contract for model and
// whether the model has one at all.
func ZhipuEffortContract(model string) (ZhipuEffort, bool) {
	contract, ok := zhipuEffortContracts[strings.ToLower(strings.TrimSpace(model))]
	return contract, ok
}

// ZhipuDepthModel recognizes only model IDs whose direct API effort contract is
// documented. A gateway or an unrelated GLM variant must not inherit it by name.
func ZhipuDepthModel(model string) string {
	model = strings.ToLower(strings.TrimSpace(model))
	if _, ok := ZhipuEffortContract(model); ok {
		return model
	}
	return ""
}

// ForcesThinking reports whether the model runs thinking unconditionally, so
// even its lowest level thinks (and is billed). True for a depth model with no
// documented way to switch thinking off.
func (z ZhipuEffort) ForcesThinking() bool { return len(z.ThinkingOff) == 0 }

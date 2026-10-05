package openai

import (
	"fmt"
	"strings"

	"reasonix/internal/contract/provider"
)

// resolveZhipuEffort validates the configured effort against the model's
// documented contract (provider.ZhipuEffortContract,
// https://docs.z.ai/guides/overview/concept-param) and folds the legacy binary
// spellings onto it: `enabled` becomes the contract default, `disabled` the
// level the model still accepts when thinking cannot be turned off.
func resolveZhipuEffort(name, model, effort string) (string, error) {
	contract, ok := provider.ZhipuEffortContract(model)
	if !ok {
		switch effort {
		case "", "enabled", "disabled":
			return effort, nil
		default:
			return "", fmt.Errorf("openai: provider %q uses Zhipu thinking; effort must be enabled or disabled", name)
		}
	}
	switch effort {
	case "":
		return effort, nil
	case "enabled":
		return contract.Default, nil
	case "disabled":
		return contract.DisabledTo, nil
	}
	if supportsEffort(contract.Levels, effort) {
		return effort, nil
	}
	return "", fmt.Errorf("openai: provider %q: effort must be %s", name, strings.Join(contract.Levels, ", "))
}

// glmThinkingEnabled reports whether this GLM request runs with thinking on.
// GLM-5.3 always thinks (the model cannot disable it); GLM-5.2 keeps thinking
// off when the configured effort is one of the contract's thinking-off levels
// or an explicit `thinking = "disabled"` was set.
func (c *client) glmThinkingEnabled() bool {
	if c == nil || !c.zhipu {
		return false
	}
	contract, ok := provider.ZhipuEffortContract(c.zhipuDepth)
	if ok && contract.ForcesThinking() {
		return true
	}
	if ok && supportsEffort(contract.ThinkingOff, c.effort) {
		return false
	}
	t := c.effort
	if c.thinkingType != "" {
		t = c.thinkingType
	}
	return t != "disabled"
}

// applyZhipuEffort writes the Zhipu thinking knob and reasoning depth onto the
// request. A model with a depth contract drives thinking.type and
// reasoning_effort together; the rest keep the binary thinking.type and omit
// reasoning_effort entirely.
func (c *client) applyZhipuEffort(out *chatRequest, req provider.Request) {
	contract, ok := provider.ZhipuEffortContract(c.zhipuDepth)
	if !ok {
		t := c.effort
		if t == "" {
			t = "enabled"
		}
		if c.thinkingType != "" {
			t = c.thinkingType
		}
		out.Thinking = &thinkingMode{Type: t}
		out.ReasoningEffort = ""
		return
	}
	depth := c.requestEffort(req)
	switch {
	case contract.ForcesThinking():
		// GLM-5.3 cannot disable thinking, so a persisted off switch becomes
		// the cheapest level the model does accept.
		out.Thinking = &thinkingMode{Type: "enabled"}
		if c.thinkingType == "disabled" {
			depth = contract.DisabledTo
		}
	case c.thinkingType == "disabled" || supportsEffort(contract.ThinkingOff, depth):
		out.Thinking = &thinkingMode{Type: "disabled"}
		depth = ""
	default:
		out.Thinking = &thinkingMode{Type: "enabled"}
	}
	out.ReasoningEffort = depth
}

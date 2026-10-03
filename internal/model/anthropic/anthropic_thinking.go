package anthropic

import "reasonix/internal/contract/provider"

// applyThinkingProfile sets the provider-specific thinking fields. DeepSeek
// defaults to enabled and accepts output_config.effort alongside its binary
// toggle. Adaptive reaches here only for endpoints under Anthropic's contract
// (resolveReasoning); LongCat-style gateways take enabled|disabled and reject
// output_config.
func (c *client) applyThinkingProfile(r *anthRequest, req provider.Request) {
	if c.deepseek {
		r.Temperature = req.Temperature
		t := c.thinking
		if t != "disabled" {
			t = "enabled"
		}
		if c.effort == "disabled" {
			t = "disabled"
		}
		r.Thinking = &thinkingConfig{Type: t}
		if t != "disabled" {
			effort := normalizeDeepSeekAnthropicEffort(c.model, c.effort)
			switch effort {
			case "low", "high", "max":
				r.OutputConfig = &outputConfig{Effort: effort}
			}
		}
	} else {
		t := c.thinking
		if c.effort == "enabled" || c.effort == "disabled" {
			t = c.effort // on a binary endpoint /effort is the toggle itself
		}
		switch t {
		case "adaptive":
			r.Thinking = &thinkingConfig{Type: "adaptive", Display: "summarized"}
			if c.effort != "" {
				r.OutputConfig = &outputConfig{Effort: c.effort}
			}
		case "enabled", "disabled":
			r.Thinking = &thinkingConfig{Type: t}
		}
	}
}

package anthropic

import (
	"strings"

	"reasonix/internal/contract/provider"
	"reasonix/internal/model/openai"
)

func resolveMiniMaxReasoning(cfg provider.Config, thinking, effort string) (string, string, error) {
	protocol, _ := cfg.Extra["reasoning_protocol"].(string)
	if !provider.IsMiniMaxEndpoint(cfg.BaseURL) || (protocol != "" && protocol != "auto") {
		return thinking, effort, nil
	}
	rawEffort, _ := cfg.Extra["effort"].(string)
	if provider.IsMiniMaxFlash(cfg.Model) {
		depth, err := openai.MiniMaxFlashEffort(rawEffort, cfg.Extra)
		return "adaptive", depth, err
	}
	if strings.EqualFold(cfg.Model, "MiniMax-M3") {
		mode, _ := cfg.Extra["thinking"].(string)
		if rawEffort == "adaptive" || rawEffort == "disabled" {
			mode = rawEffort
		}
		if mode == "adaptive" || mode == "enabled" {
			return "adaptive", "", nil
		}
		return "disabled", "", nil
	}
	return thinking, effort, nil
}

func (c *client) RequiresReasoningRoundTrip() bool {
	return c != nil && provider.IsMiniMaxEndpoint(c.baseURL)
}

func (c *client) adaptiveThinkingConfig() *thinkingConfig {
	out := &thinkingConfig{Type: "adaptive", Display: "summarized"}
	if provider.IsMiniMaxEndpoint(c.baseURL) {
		out.Display = ""
	}
	return out
}

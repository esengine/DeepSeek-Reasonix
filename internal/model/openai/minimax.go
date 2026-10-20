package openai

import (
	"fmt"
	"slices"
	"strings"

	"reasonix/internal/contract/provider"
)

// MiniMaxFlashEffort validates the shared MiniMax depth contract for both API wires.
func MiniMaxFlashEffort(effort string, extra map[string]any) (string, error) {
	effort = strings.ToLower(strings.TrimSpace(effort))
	thinking, _ := extra["thinking"].(string)
	thinking = strings.ToLower(strings.TrimSpace(thinking))
	if thinking == "disabled" || thinking == "off" || thinking == "none" {
		return "", fmt.Errorf("MiniMax-M3.1-Flash cannot disable thinking: %w", provider.ErrEffortRefused)
	}
	if effort == "" || effort == "auto" {
		return "max", nil
	}
	if !slices.Contains(provider.MiniMaxFlashEfforts(), effort) {
		return "", fmt.Errorf("MiniMax-M3.1-Flash effort must be low, medium, high, xhigh, or max: %w", provider.ErrEffortRefused)
	}
	return effort, nil
}

func (c *client) applyMiniMaxThinking(out *chatRequest, req provider.Request) {
	if provider.IsMiniMaxFlash(c.model) {
		out.Thinking = &thinkingMode{Type: "adaptive"}
		out.ReasoningEffort = c.requestEffort(req)
		out.ExtraBody = omitExtraBodyFields(out.ExtraBody, "thinking", "reasoning_effort", "service_tier", "reasoning_split")
		if out.ExtraBody == nil {
			out.ExtraBody = map[string]any{}
		}
		out.ExtraBody["reasoning_split"] = true
		return
	}
	// M3 exposes only a binary thinking knob; saved older models keep that path.
	mode := c.effort
	if mode == "" {
		mode = "adaptive"
	}
	out.Thinking = &thinkingMode{Type: mode}
	out.ReasoningEffort = ""
}

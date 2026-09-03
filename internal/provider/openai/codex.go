package openai

import (
	"strings"

	"reasonix/internal/provider"
)

// modelIsCodex reports whether the model id is one of OpenAI's Codex family
// (gpt-5-codex, gpt-5.1-codex-mini, ...). The token is OpenAI-specific, so the
// substring match can't misfire on unrelated gateway model ids.
func modelIsCodex(model string) bool {
	return strings.Contains(strings.ToLower(strings.TrimSpace(model)), "codex")
}

// wireRole serializes a message role for the wire. Codex models reject the
// system role (their system prompt lives in the Responses API instructions
// field), so their requests carry the system prompt as a user message instead
// (LiteLLM translates the same way for supports_system_messages=false models).
func (c *client) wireRole(r provider.Role) string {
	if modelIsCodex(c.model) && r == provider.RoleSystem {
		return string(provider.RoleUser)
	}
	return string(r)
}

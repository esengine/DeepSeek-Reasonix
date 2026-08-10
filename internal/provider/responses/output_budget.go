package responses

import "reasonix/internal/provider"

// OutputBudget reports the default total output budget sent by this client.
func (c *client) OutputBudget() int { return c.maxOutputTokens }

// SharesContextWindow is true only for the official DeepSeek vendor mode.
func (c *client) SharesContextWindow() bool { return c.vendor == "deepseek" }

// SharedWindowInputPolicy reports the Responses-only history items that the
// official DeepSeek adapter replays into later requests.
func (c *client) SharedWindowInputPolicy() provider.SharedWindowInputPolicy {
	replay := c.vendor == "deepseek"
	return provider.SharedWindowInputPolicy{
		ReplaysOrdinaryReasoning: replay,
		ReplaysResponsesItems:    replay,
	}
}

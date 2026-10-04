package responses

import "log/slog"

type continuationState struct {
	disabled bool
}

func (c *client) effectiveModeLocked() string {
	if c.continuation.disabled {
		return "stateless"
	}
	return c.mode
}

func (c *client) disableContinuation() {
	c.mu.Lock()
	c.continuation.disabled = true
	c.lastResponseID, c.expectedPrefixDigest = "", ""
	c.mu.Unlock()
	slog.Debug("Responses continuation disabled after full-history recovery", "provider", c.name, "configured_mode", c.mode, "effective_mode", "stateless")
}

package responses

import "strings"

// apiKeyResolver mirrors provider.Config.APIKeyResolver: a request needs the key
// now, not the key this client was built with, and an unreadable credential
// falls back rather than breaking the run.
func (c Config) apiKeyResolver() func() string {
	static := c.APIKey
	if c.APIKeyFunc == nil {
		return func() string { return static }
	}
	return func() string {
		if key := strings.TrimSpace(c.APIKeyFunc()); key != "" {
			return key
		}
		return static
	}
}

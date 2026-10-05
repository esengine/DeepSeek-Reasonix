package provider

import "strings"

// Config is a resolved provider instance configuration.
type Config struct {
	Name    string // instance name, e.g. "deepseek"
	BaseURL string // OpenAI-compatible endpoint
	Model   string // model id
	APIKey  string // resolved from api_key_env
	// APIKeyFunc, when set, answers per request instead of APIKey: a key is
	// external mutable state, and replacing an exhausted one must not require
	// rebuilding the session. An empty answer falls back to APIKey.
	APIKeyFunc func() string
	Extra      map[string]any // kind-specific options
}

// APIKeyResolver returns the key source a provider should consult per request:
// the live one when configured, otherwise the value fixed at construction.
func (c Config) APIKeyResolver() func() string {
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

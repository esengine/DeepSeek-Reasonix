package boot

import (
	"reasonix/internal/base/netclient"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/provider"
)

// NewProviderWithProxy builds a provider.Provider with the configured ordinary
// network proxy settings.
func NewProviderWithProxy(e *config.ProviderEntry, proxy netclient.ProxySpec) (provider.Provider, error) {
	return provider.New(e.Kind, providerConfig(e, proxy))
}

func providerConfig(e *config.ProviderEntry, proxy netclient.ProxySpec) provider.Config {
	return provider.Config{
		Name:    e.Name,
		BaseURL: e.BaseURL,
		Model:   e.Model,
		APIKey:  e.APIKey(), APIKeyFunc: e.APIKey, // live: a replaced key reaches the next request
		// Pass the key's env var so auth failures can name where to fix it, plus
		// provider-kind-specific knobs. EffectiveEffort applies a configured
		// default_effort when the user has not explicitly selected /effort.
		Extra: map[string]any{
			"api_key_env":            e.APIKeyEnv,
			"api_key_source":         e.APIKeySourceLabel(),
			"thinking":               e.Thinking,
			"effort":                 config.EffectiveEffort(e),
			"supported_efforts":      e.SupportedEfforts,
			"reasoning_protocol":     config.ReasoningProtocolForEntry(e),
			"strip_chain_of_thought": e.StripChainOfThought,
			"max_output_tokens":      e.MaxOutputTokens,
			"chat_url":               e.ChatURL,
			"request_url":            e.RequestURL,
			"headers":                e.Headers,
			"extra_body":             e.ExtraBody,
			"auth_header":            e.AuthHeader,
			"proxy_spec":             proxy,
			"vision":                 config.EffectiveVision(e),
			"vision_detail":          e.VisionDetail,
			"web_search":             config.EffectiveWebSearch(e),
			"mode":                   e.ResponsesMode,
			// Keep nil as nil so the responses provider can vendor-detect its
			// default instead of accidentally treating every endpoint as stateful.
			"stateful": e.ResponsesStateful,
		},
	}
}

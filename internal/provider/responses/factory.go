package responses

import (
	"reasonix/internal/netclient"
	"reasonix/internal/provider"
)

func newFromConfig(cfg provider.Config) (provider.Provider, error) {
	cfg = provider.ApplyOpenCodeGoContract("responses", cfg)
	effort, _ := cfg.Extra["effort"].(string)
	mode, _ := cfg.Extra["mode"].(string)
	webSearch, _ := cfg.Extra["web_search"].(bool)
	var stateful *bool
	switch value := cfg.Extra["stateful"].(type) {
	case bool:
		stateful = &value
	case *bool:
		stateful = value
	}
	proxy, _ := cfg.Extra["proxy_spec"].(netclient.ProxySpec)
	keyEnv, _ := cfg.Extra["api_key_env"].(string)
	keySource, _ := cfg.Extra["api_key_source"].(string)
	maxOutputTokens, _ := cfg.Extra["max_output_tokens"].(int)
	requestURL, _ := cfg.Extra["request_url"].(string)
	return New(Config{
		HTTPClient: cfg.HTTPClient,
		Name:       cfg.Name, DisplayName: cfg.DisplayName, Protocol: cfg.Protocol, APIKey: cfg.APIKey, BaseURL: cfg.BaseURL, Model: cfg.Model,
		ModelInfo: cfg.ModelInfo,
		Effort:    effort, Mode: mode, Stateful: stateful, WebSearch: webSearch, Proxy: proxy,
		KeyEnv: keyEnv, KeySource: keySource, MaxOutputTokens: maxOutputTokens, RequestURL: requestURL,
		// Extra 原样透传：vision 等能力开关由调用方（boot/CLI）写入
		// cfg.Extra，factory 若丢弃则 New() 读不到（评审 #7234 第 3 点）。
		Extra: cfg.Extra,
	}), nil
}

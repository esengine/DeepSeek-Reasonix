package config

import "strings"

// SystemOneConfig connects the system_one tool to TypeSafe AI's decision API.
type SystemOneConfig struct {
	BaseURL   string     `toml:"base_url"`
	Model     string     `toml:"model"`
	APIKeyEnv string     `toml:"api_key_env"`
	Laya      LayaConfig `toml:"laya"`
}

type LayaConfig struct {
	Local         bool   `toml:"local"`
	Python        string `toml:"python"`
	Model         string `toml:"model"`
	HTTPBaseURL   string `toml:"http_base_url"`
	HTTPAPIKeyEnv string `toml:"http_api_key_env"`
}

func (c *Config) SystemOneAPIKey() string {
	if c == nil {
		return ""
	}
	key := strings.TrimSpace(c.Tools.SystemOne.APIKeyEnv)
	if key == "" {
		key = "TYPESAFE_API_KEY"
	}
	if global := ResolveCredentialForRootGlobalFirst(".", key); global.Set {
		return global.Value
	}
	return ResolveCredentialForRoot(".", key).Value
}

func (c *Config) LayaHTTPAPIKey() string {
	if c == nil || strings.TrimSpace(c.Tools.SystemOne.Laya.HTTPAPIKeyEnv) == "" {
		return ""
	}
	key := strings.TrimSpace(c.Tools.SystemOne.Laya.HTTPAPIKeyEnv)
	if global := ResolveCredentialForRootGlobalFirst(".", key); global.Set {
		return global.Value
	}
	return ResolveCredentialForRoot(".", key).Value
}

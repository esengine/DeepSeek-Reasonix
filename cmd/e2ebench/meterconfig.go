package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// meterUpstream reports the endpoint the meter must forward to: the base_url
// of the provider serving the benchmarked model. Only that provider is ever
// redirected — rewriting every endpoint would send one vendor's traffic to
// another's host.
func meterUpstream(configPath, model string) (upstream string, err error) {
	raw, err := os.ReadFile(configPath)
	if err != nil {
		return "", fmt.Errorf("read config for metering: %w", err)
	}
	var doc map[string]any
	if err := toml.Unmarshal(raw, &doc); err != nil {
		return "", fmt.Errorf("parse %s: %w", configPath, err)
	}
	providers, _ := doc["providers"].([]map[string]any)
	if len(providers) == 0 {
		if list, ok := doc["providers"].([]any); ok {
			for _, entry := range list {
				if p, ok := entry.(map[string]any); ok {
					providers = append(providers, p)
				}
			}
		}
	}
	if len(providers) == 0 {
		return "", fmt.Errorf("%s declares no [[providers]]; the meter has nothing to redirect", configPath)
	}
	target := providerForModel(providers, model)
	upstream, _ = target["base_url"].(string)
	if strings.TrimSpace(upstream) == "" {
		return "", fmt.Errorf("provider %v has no base_url to redirect", target["name"])
	}
	return upstream, nil
}

// providerForModel picks the entry that serves model, falling back to the
// first. A benchmark run is single-model, so an exact match is the common case
// and the fallback only matters for a one-provider config.
func providerForModel(providers []map[string]any, model string) map[string]any {
	model = strings.TrimSpace(model)
	if model != "" {
		if _, want, ok := strings.Cut(model, "/"); ok {
			model = want
		}
		for _, p := range providers {
			if name, _ := p["default"].(string); name == model {
				return p
			}
			list, _ := p["models"].([]any)
			for _, m := range list {
				if s, _ := m.(string); s == model {
					return p
				}
			}
			if s, _ := p["model"].(string); s == model {
				return p
			}
		}
	}
	return providers[0]
}

// writeMeteredConfig rewrites the chosen provider's base_url to meterBase and
// writes the result into dir as config.toml.
func writeMeteredConfig(configPath, dir, model, meterBase string) error {
	raw, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}
	var doc map[string]any
	if err := toml.Unmarshal(raw, &doc); err != nil {
		return err
	}
	var providers []map[string]any
	switch list := doc["providers"].(type) {
	case []map[string]any:
		providers = list
	case []any:
		for _, entry := range list {
			if p, ok := entry.(map[string]any); ok {
				providers = append(providers, p)
			}
		}
	}
	if len(providers) == 0 {
		return fmt.Errorf("%s declares no [[providers]]", configPath)
	}
	target := providerForModel(providers, model)
	target["base_url"] = meterBase
	// The redirected endpoint is loopback plaintext; a proxy configured for the
	// real vendor host must not be applied to it.
	target["no_proxy"] = true
	doc["providers"] = providers

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	out, err := os.Create(filepath.Join(dir, "config.toml"))
	if err != nil {
		return err
	}
	defer out.Close()
	return toml.NewEncoder(out).Encode(doc)
}

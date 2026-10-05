// provider_keyreuse.go — a second door onto an account already signed in.
package serve

import (
	"strings"

	"reasonix/internal/contract/config"
)

// keyEnvForNewSource decides which credential slot a new source writes. A blank
// key at a host that already has one is another door onto that account, so the
// two share a slot; replacing an entry at its own host keeps that entry's slot.
// Anything else gets a slot nothing holds, so it neither overwrites a key nor
// reads one stored for another endpoint.
func keyEnvForNewSource(cfg *config.Config, name, baseURL, apiKey string, replace bool) (string, error) {
	host := vendorOf(baseURL)
	if old, ok := cfg.Provider(name); replace && ok && host != "" && vendorOf(old.BaseURL) == host && strings.TrimSpace(old.APIKeyEnv) != "" {
		return old.APIKeyEnv, nil
	}
	if strings.TrimSpace(apiKey) == "" && host != "" {
		for i := range cfg.Providers {
			p := &cfg.Providers[i]
			if p.Name != name && vendorOf(p.BaseURL) == host && strings.TrimSpace(p.APIKeyEnv) != "" {
				return p.APIKeyEnv, nil
			}
		}
	}
	return config.FreeAPIKeyEnvFor(name, cfg.Providers)
}

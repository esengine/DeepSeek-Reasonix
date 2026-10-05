// legacy_providers.go — folding a provider's superseded fields into the ones
// that replaced them, once at load, so nothing downstream resolves two forms.
package config

import "strings"

// normalizeLegacyProviderFields runs every fold, reporting whether it changed
// anything. The rename goes first: a fold keyed by a retired name files the
// user's price and window under a key the template also fills, leaving the
// rename to choose between two values nothing tells apart.
func normalizeLegacyProviderFields(c *Config) bool {
	changed := migrateRetiredDeepSeekModels(c)
	normalizeLegacyProviderModels(c)
	normalizeLegacyResponsesMode(c)
	canonicalizeOfficialDeepSeekSource(c)
	foldLegacyDeepSeekPeers(c)
	return changed
}

// normalizeLegacyResponsesMode folds responses_stateful into responses_mode, the
// field that replaced it. Downstream then reads one field instead of resolving
// two, and a nil stays nil so an endpoint with neither set still gets the
// vendor detection it depends on.
func normalizeLegacyResponsesMode(c *Config) {
	if c == nil {
		return
	}
	for i := range c.Providers {
		p := &c.Providers[i]
		if p.ResponsesStateful == nil {
			continue
		}
		// responses_mode already won when both were present; folding keeps that
		// order rather than letting the older field overwrite the newer one.
		if mode := strings.ToLower(strings.TrimSpace(p.ResponsesMode)); mode != "stateful" && mode != "stateless" {
			if *p.ResponsesStateful {
				p.ResponsesMode = "stateful"
			} else {
				p.ResponsesMode = "stateless"
			}
		}
		p.ResponsesStateful = nil
	}
}

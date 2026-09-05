package config

import "strings"

// WithAPIKeyForProbe returns a private copy with a draft credential. It neither
// persists the value nor mutates process environment shared by other requests.
func (e ProviderEntry) WithAPIKeyForProbe(value string) ProviderEntry {
	e.resolvedAPIKey = strings.TrimSpace(value)
	e.resolvedSource = CredentialSource{Kind: CredentialSourceEnvironment, Label: "settings probe"}
	return e
}

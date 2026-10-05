package config

import "strings"

// WithAPIKeyForProbe returns an ephemeral copy carrying a user-entered key.
// The copy cannot write the key through config rendering or credential storage.
func (e *ProviderEntry) WithAPIKeyForProbe(value string) ProviderEntry {
	if e == nil {
		return ProviderEntry{}
	}
	out := *e
	out.resolvedAPIKey = strings.TrimSpace(value)
	out.resolvedSource = CredentialSource{Kind: CredentialSourceEnvironment, Label: "settings prompt"}
	return out
}

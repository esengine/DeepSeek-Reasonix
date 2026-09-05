package config

import (
	"crypto/sha256"
	"fmt"
)

// ModelCapabilitySnapshot identifies image routing across the configured models,
// including potential image-understanding fallbacks. It is never provider-visible.
func ModelCapabilitySnapshot(cfg *Config, resolver *ModelCapabilityResolver) string {
	h := sha256.New()
	credentialsRevision := resolver.credentialRevision()
	_, _ = fmt.Fprintf(h, "%q", cfg.Agent.VisionModel)
	for _, entry := range cfg.Providers {
		_, _ = fmt.Fprintf(h, "%q", resolver.providerFingerprintForCredentialRevision(entry, credentialsRevision))
		for _, model := range entry.ModelList() {
			selected := entry
			selected.Model = model
			capability := resolver.resolveWithCredentialRevision(&selected, credentialsRevision)
			_, _ = fmt.Fprintf(h, "%q:%q", model, capability.State)
		}
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

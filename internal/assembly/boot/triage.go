package boot

import (
	"log/slog"
	"strings"

	"reasonix/internal/base/netclient"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/provider"
)

// resolveTriage builds the model that answers host classifications the static
// tables could not make: triage_model, then subagent_model, then the main model,
// which is what makes the escalation work with no configuration. The instance is
// separate so classifications never enter the turn's own stream, and ref and
// pricing travel with it because triage billed at the main tier looks expensive.
func resolveTriage(cfg *config.Config, mainRef string, proxySpec netclient.ProxySpec) (provider.Provider, string, *provider.Pricing) {
	ref := firstNonEmpty(
		strings.TrimSpace(cfg.Agent.TriageModel),
		strings.TrimSpace(cfg.Agent.SubagentModel),
		strings.TrimSpace(mainRef),
	)
	if ref == "" {
		return nil, "", nil
	}
	entry, ok := cfg.ResolveModel(ref)
	if !ok {
		return nil, "", nil
	}
	prov, err := NewProviderWithProxy(entry, proxySpec)
	if err != nil {
		// Not fatal: the static verdict stands, which only ever refuses more.
		slog.Warn("triage provider construction failed — host keeps its static command classification", "model", ref, "err", err)
		return nil, "", nil
	}
	return prov, ref, entry.Price
}

package boot

import (
	"errors"
	"log/slog"
	"strings"

	"reasonix/internal/base/netclient"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/runtime/commitmsg"
	"reasonix/internal/runtime/goaleval"
	"reasonix/internal/runtime/promptrefine"
)

// goalEvaluator uses the same zero-config model fallback as the recovery
// reviewer (recovery_model → guardian_model → main model), isolated session and
// policy. When unavailable, Goal turns without an update_goal report fail
// closed and pause instead of defaulting to continue.
func goalEvaluator(cfg *config.Config, modelRef string, proxy netclient.ProxySpec, sink event.Sink) goaleval.Evaluator {
	evalModel := strings.TrimSpace(cfg.Agent.RecoveryModel)
	if evalModel == "" {
		evalModel = strings.TrimSpace(cfg.Agent.GuardianModel)
	}
	if evalModel == "" {
		evalModel = modelRef
	}
	if evalModel == "" {
		return nil
	}
	re, ok := cfg.ResolveModel(evalModel)
	if !ok {
		return nil
	}
	if cfg.Agent.RecoveryModel == "" && cfg.Agent.GuardianModel != "" {
		if effort := roleEffort(cfg, "guardian"); effort != nil {
			re.Effort = *effort
		}
	}
	eProv, err := NewProviderWithProxy(re, proxy)
	if err != nil {
		slog.Warn("goal evaluator provider construction failed — goals without an update_goal report will pause", "model", evalModel, "err", err)
		return nil
	}
	return goaleval.NewSessionWithSink(eProv, re.Price, modelRefFromEntry(re), sink)
}

// newSideProvider is the construction seam tests count calls through.
var newSideProvider = provider.New

// sideProvider builds the provider for a one-shot side task on the entry the
// session resolved, not a name resolved again: an alias or a preset reference
// names no entry of its own. Reasoning is off because the person is waiting; an
// endpoint that refuses "disabled" gets the lowest effort its vocabulary lists.
func sideProvider(e *config.ProviderEntry, proxy netclient.ProxySpec) (provider.Provider, error) {
	if e == nil || !e.Configured() {
		return nil, errSideModelUnconfigured
	}
	pc := providerConfig(e, proxy)
	pc.Extra["effort"] = "disabled"
	prov, err := newSideProvider(e.Kind, pc)
	if err == nil || !errors.Is(err, provider.ErrEffortRefused) {
		return prov, err
	}
	pc.Extra["effort"] = lowestEffort(config.RequestEffortLevels(e))
	prov, retryErr := newSideProvider(e.Kind, pc)
	if retryErr != nil {
		return nil, errors.Join(err, retryErr)
	}
	return prov, nil
}

var errSideModelUnconfigured = errors.New("side model: entry is not configured")

var effortRank = map[string]int{"minimal": 0, "low": 1, "medium": 2, "high": 3, "xhigh": 4, "max": 5}

// lowestEffort is the cheapest level of a vocabulary, or "" (the provider's
// automatic depth) when it names none the scale knows.
func lowestEffort(levels []string) string {
	best, bestRank := "", len(effortRank)
	for _, l := range levels {
		if r, ok := effortRank[strings.ToLower(strings.TrimSpace(l))]; ok && r < bestRank {
			best, bestRank = strings.ToLower(strings.TrimSpace(l)), r
		}
	}
	return best
}

// promptRefiner rewrites drafts with the session's resolved entry.
func promptRefiner(e *config.ProviderEntry, proxy netclient.ProxySpec, sink event.Sink) *promptrefine.Refiner {
	prov, err := sideProvider(e, proxy)
	if err != nil {
		if !errors.Is(err, errSideModelUnconfigured) {
			slog.Warn("prompt refiner provider construction failed", "model", modelRefFromEntry(e), "err", err)
		}
		return nil
	}
	return promptrefine.New(prov, e.Price, modelRefFromEntry(e), sink)
}

// commitMessenger writes commit messages with the session's resolved entry.
func commitMessenger(e *config.ProviderEntry, proxy netclient.ProxySpec, sink event.Sink) *commitmsg.Generator {
	prov, err := sideProvider(e, proxy)
	if err != nil {
		if !errors.Is(err, errSideModelUnconfigured) {
			slog.Warn("commit messenger provider construction failed", "model", modelRefFromEntry(e), "err", err)
		}
		return nil
	}
	return commitmsg.New(prov, e.Price, modelRefFromEntry(e), sink)
}

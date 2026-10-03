package boot

import (
	"log/slog"
	"strings"

	"reasonix/internal/base/netclient"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
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

// promptRefiner rewrites drafts with the entry the session resolved, not a name
// resolved again: an alias or a preset reference names no entry of its own.
// Reasoning is off, because the rewrite is short and the person is waiting.
func promptRefiner(e *config.ProviderEntry, proxy netclient.ProxySpec, sink event.Sink) *promptrefine.Refiner {
	if e == nil || !e.Configured() {
		return nil
	}
	pc := providerConfig(e, proxy)
	pc.Extra["effort"] = "disabled"
	prov, err := provider.New(e.Kind, pc)
	if err != nil {
		slog.Debug("prompt refiner provider construction failed", "model", modelRefFromEntry(e), "err", err)
		return nil
	}
	return promptrefine.New(prov, e.Price, modelRefFromEntry(e), sink)
}

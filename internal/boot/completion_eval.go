package boot

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"reasonix/internal/agent"
	"reasonix/internal/completioneval"
	"reasonix/internal/config"
	"reasonix/internal/event"
	"reasonix/internal/netclient"
	"reasonix/internal/provider"
)

// completionEval wires the completion validator into agent/task options.
type completionEval struct {
	factory agent.CompletionEvaluatorFactory
	mode    string
}

func newCompletionEval(cfg *config.Config, resolver provider.Resolver, proxySpec netclient.ProxySpec) completionEval {
	mode := cfg.Agent.CompletionValidationMode()
	return completionEval{factory: newCompletionEvalFactory(cfg, resolver, proxySpec), mode: mode}
}

func (c completionEval) options(opts agent.Options) agent.Options {
	opts.CompletionEvaluatorFactory = c.factory
	opts.CompletionValidation = c.mode
	return opts
}

func (c completionEval) taskOptions(opts agent.TaskToolOptions) agent.TaskToolOptions {
	opts.CompletionEvaluatorFactory = c.factory
	opts.CompletionValidation = c.mode
	return opts
}

// newCompletionEvalFactory resolves each evaluator from the Agent's effective
// model unless completion_evaluator_model explicitly selects another target.
func newCompletionEvalFactory(cfg *config.Config, resolver provider.Resolver, proxySpec netclient.ProxySpec) agent.CompletionEvaluatorFactory {
	mode := cfg.Agent.CompletionValidationMode()
	if mode == config.CompletionValidationOff {
		return nil
	}
	return func(modelRef string, sink event.Sink) completioneval.Evaluator {
		evalRef := strings.TrimSpace(modelRef)
		if configured := strings.TrimSpace(cfg.Agent.CompletionEvaluatorModel); configured != "" {
			evalRef = configured
		}
		entry, selectedRef, ok := completionEvalTarget(cfg, resolver, evalRef)
		if !ok {
			err := fmt.Errorf("model %q not found", evalRef)
			slog.Warn("completion evaluator unavailable", "model", evalRef, "err", err)
			return unavailableCompletionEvaluator{err: fmt.Errorf("completion evaluator unavailable: %w", err)}
		}
		cProv, err := resolveProvider(resolver, cfg, proxySpec, provider.Selection{Ref: selectedRef})
		if err != nil {
			slog.Warn("completion evaluator unavailable", "model", evalRef, "err", err)
			return unavailableCompletionEvaluator{err: fmt.Errorf("completion evaluator unavailable: %w", err)}
		}
		return completioneval.NewSession(cProv, entry.Price, modelRefFromEntry(entry), sink)
	}
}

func completionEvalTarget(cfg *config.Config, resolver provider.Resolver, ref string) (*config.ProviderEntry, string, bool) {
	if resolver != nil {
		if entry := syntheticEntryFromResolver(resolver, ref); strings.TrimSpace(entry.Name) != "" {
			return entry, modelRefFromEntry(entry), true
		}
	}
	entry, ok := cfg.ResolveModel(ref)
	if !ok {
		return nil, ref, false
	}
	return entry, modelRefFromEntry(entry), true
}

type unavailableCompletionEvaluator struct{ err error }

func (e unavailableCompletionEvaluator) Evaluate(context.Context, completioneval.Evidence) (completioneval.Verdict, error) {
	return completioneval.Verdict{}, e.err
}

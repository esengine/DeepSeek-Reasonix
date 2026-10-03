package boot

import (
	"fmt"
	"strings"

	"reasonix/internal/base/netclient"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/provider"
	"reasonix/internal/ext/skill"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/runtime/delegation"
	"reasonix/internal/runtime/writeclaim"
)

// subagentConfig is what a build resolves about sub-agents before any of them
// runs: which provider a named model or profile lands on, the depth and
// concurrency ceilings, and the profile lookups the task tool consults.
type subagentConfig struct {
	resolveProvider func(modelRef, effort string) (provider.Provider, *provider.Pricing, int, error)
	identity        func(modelRef, effort string) (string, string)
	profileLookup   func(name string) (delegation.ProfileDefinition, bool)
	profileModel    func(profile string) string
	profileEffort   func(profile string) string
	scheduler       *writeclaim.SubagentScheduler
	taskModel       string
	taskEffort      string
	maxDepth        int
}

func newSubagentConfig(opts Options, cfg *config.Config, entry *config.ProviderEntry, modelName string,
	resolver provider.Resolver, proxy netclient.ProxySpec, skills *skill.Store) subagentConfig {
	maxConcurrency, maxWriters := writeclaim.NormalizeConcurrencyLimits(
		cfg.Agent.MaxSubagentConcurrency, cfg.Agent.MaxParallelWriters,
	)
	return subagentConfig{
		resolveProvider: func(modelRef, effort string) (provider.Provider, *provider.Pricing, int, error) {
			me := *entry
			selectedRef := modelRefFromEntry(entry)
			if strings.TrimSpace(modelRef) != "" {
				modelRef = childModelRef(cfg, entry, modelRef)
				if resolved, ok := cfg.ResolveModel(modelRef); ok {
					me = *resolved
					selectedRef = modelRefFromEntry(resolved)
				} else if resolver != nil {
					me = *syntheticEntryFromResolver(resolver, modelRef)
					selectedRef = modelRef
				} else {
					return nil, nil, 0, fmt.Errorf("unknown model %q", modelRef)
				}
			}
			var effortOverride *string
			if strings.TrimSpace(effort) != "" {
				normalized, err := config.NormalizeEffort(&me, effort)
				if err != nil {
					if resolver == nil {
						return nil, nil, 0, err
					}
					normalized = effort
				}
				me.Effort = normalized
				effortOverride = &normalized
				if me.Kind == "anthropic" && strings.TrimSpace(me.Effort) != "" && strings.TrimSpace(me.Thinking) == "" {
					me.Thinking = "adaptive"
				}
			}
			p, err := resolveProvider(resolver, cfg, proxy, provider.Selection{Ref: selectedRef, Effort: effortOverride})
			if err != nil {
				return nil, nil, 0, err
			}
			return p, me.Price, me.ContextWindow, nil
		},
		identity: func(modelRef, effort string) (string, string) {
			return subagentEffectiveIdentity(cfg, opts.ProviderResolver, modelName, entry, modelRef, effort)
		},
		profileLookup: func(name string) (delegation.ProfileDefinition, bool) {
			sk, ok := skills.Read(name)
			if !ok || sk.RunAs != skill.RunSubagent {
				return delegation.ProfileDefinition{}, false
			}
			return delegation.ProfileFromSkill(skills.Prepare(sk)), true
		},
		profileModel:  func(profile string) string { return firstConfigured(cfg.Agent.SubagentModels, profile) },
		profileEffort: func(profile string) string { return firstConfigured(cfg.Agent.SubagentEfforts, profile) },
		scheduler:     writeclaim.NewSubagentScheduler(maxConcurrency, maxWriters),
		taskModel:     firstNonEmpty(cfg.Agent.SubagentModels["task"], cfg.Agent.SubagentModel),
		taskEffort:    firstNonEmpty(cfg.Agent.RoleEfforts["subagent"], cfg.Agent.SubagentEfforts["task"], cfg.Agent.SubagentEffort, "auto"),
		maxDepth:      agent.NormalizeMaxSubagentDepth(cfg.Agent.MaxSubagentDepth),
	}
}

// childModelRef qualifies a bare model name with the parent's provider when
// that provider serves it. Config resolves a bare name to the first provider
// listing it, which would move a child onto a provider the user never picked.
func childModelRef(cfg *config.Config, parent *config.ProviderEntry, ref string) string {
	ref = strings.TrimSpace(ref)
	if cfg == nil || parent == nil || ref == "" || strings.Contains(ref, "/") || !parent.HasModel(ref) {
		return ref
	}
	if _, isProvider := cfg.Provider(ref); isProvider {
		return ref
	}
	return parent.Name + "/" + ref
}

// firstConfigured returns the first non-empty value among the keys a profile
// answers to, so an alias inherits the setting written under its canonical name.
func firstConfigured(values map[string]string, profile string) string {
	for _, key := range SubagentModelKeys(profile) {
		if v := strings.TrimSpace(values[key]); v != "" {
			return v
		}
	}
	return ""
}

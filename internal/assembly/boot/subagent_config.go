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
	resolveProvider        func(modelRef, effort string) (provider.Provider, *provider.Pricing, int, error)
	identity               func(modelRef, effort string) (string, string)
	profileLookup          func(name string) (delegation.ProfileDefinition, bool)
	profileModel           func(profile string) string
	profileEffort          func(profile string) string
	scheduler              *writeclaim.SubagentScheduler
	inheritedEffort        string
	inheritedEffortDropped bool
	taskModel              string
	taskEffort             string
	maxDepth               int
}

type inheritedSubagentEffort struct {
	value   string
	dropped bool
}

func resolveInheritedSubagentEffort(cfg *config.Config, entry *config.ProviderEntry) inheritedSubagentEffort {
	if cfg == nil {
		return inheritedSubagentEffort{}
	}
	raw := strings.TrimSpace(cfg.Agent.SubagentEffort)
	if raw == "" {
		return inheritedSubagentEffort{}
	}
	if strings.TrimSpace(cfg.Agent.SubagentModel) != "" {
		return inheritedSubagentEffort{value: raw}
	}
	if normalized, ok := config.NormalizeInheritedEffort(entry, raw); ok {
		return inheritedSubagentEffort{value: normalized}
	}
	return inheritedSubagentEffort{dropped: true}
}

func newSubagentConfig(opts Options, cfg *config.Config, entry *config.ProviderEntry, modelName string,
	resolver provider.Resolver, proxy netclient.ProxySpec, skills *skill.Store) subagentConfig {
	maxConcurrency, maxWriters := writeclaim.NormalizeConcurrencyLimits(
		cfg.Agent.MaxSubagentConcurrency, cfg.Agent.MaxParallelWriters,
	)
	// The whole-workspace gate is a user setting. It ships on, so leaving the
	// package default untouched is exactly upstream behaviour.
	writeclaim.SetSerializeWholeWorkspace(cfg.Agent.SerializeOpaqueWriters)
	inherited := resolveInheritedSubagentEffort(cfg, entry)
	return subagentConfig{
		resolveProvider: func(modelRef, effort string) (provider.Provider, *provider.Pricing, int, error) {
			me, selectedRef, err := subagentModelEntry(cfg, resolver, entry, modelRef)
			if err != nil {
				return nil, nil, 0, err
			}
			var effortOverride *string
			if selectedRef == modelRefFromEntry(entry) {
				effortOverride = &me.Effort
			}
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
		profileModel:           func(profile string) string { return firstConfigured(cfg.Agent.SubagentModels, profile) },
		profileEffort:          func(profile string) string { return firstConfigured(cfg.Agent.SubagentEfforts, profile) },
		scheduler:              writeclaim.NewSubagentScheduler(maxConcurrency, maxWriters),
		inheritedEffort:        inherited.value,
		inheritedEffortDropped: inherited.dropped,
		taskModel:              firstNonEmpty(cfg.Agent.SubagentModels["task"], cfg.Agent.SubagentModel),
		taskEffort:             firstNonEmpty(cfg.Agent.SubagentEfforts["task"], inherited.value),
		maxDepth:               agent.NormalizeMaxSubagentDepth(cfg.Agent.MaxSubagentDepth),
	}
}

func subagentModelEntry(cfg *config.Config, resolver provider.Resolver, parent *config.ProviderEntry, ref string) (config.ProviderEntry, string, error) {
	ref = childModelRef(cfg, parent, ref)
	if parent != nil && (ref == "" || ref == modelRefFromEntry(parent)) {
		return *parent, modelRefFromEntry(parent), nil
	}
	if cfg != nil {
		if resolved, ok := cfg.ResolveModel(ref); ok {
			return *resolved, modelRefFromEntry(resolved), nil
		}
	}
	if resolver != nil {
		return *syntheticEntryFromResolver(resolver, ref), ref, nil
	}
	return config.ProviderEntry{}, ref, fmt.Errorf("unknown model %q", ref)
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

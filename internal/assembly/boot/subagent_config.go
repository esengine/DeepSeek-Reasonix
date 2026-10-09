package boot

import (
	"fmt"
	"slices"
	"strings"
	"sync"

	"reasonix/internal/base/netclient"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
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
	inheritedFor           func(modelRef string) string
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
	raw := firstNonEmpty(cfg.Agent.RoleEfforts["subagent"], cfg.Agent.SubagentEffort)
	if raw == "" {
		return inheritedSubagentEffort{}
	}
	if raw == "auto" || strings.TrimSpace(cfg.Agent.SubagentModel) != "" {
		return inheritedSubagentEffort{value: raw}
	}
	if normalized, ok := config.NormalizeInheritedEffort(entry, raw); ok {
		return inheritedSubagentEffort{value: normalized}
	}
	return inheritedSubagentEffort{dropped: true}
}

// inheritedEffortFor resolves agent.subagent_effort against the model that will
// execute, so a default written for the parent is never remapped onto another
// model. An empty result means that model runs at its own default; the dropped
// default is announced once per model.
func inheritedEffortFor(opts Options, cfg *config.Config, resolver provider.Resolver, parent *config.ProviderEntry, announced *sync.Map) func(string) string {
	return func(modelRef string) string {
		raw := firstNonEmpty(cfg.Agent.RoleEfforts["subagent"], cfg.Agent.SubagentEffort)
		ref := strings.TrimSpace(modelRef)
		if raw == "" {
			return ""
		}
		if ref != "" && ref == strings.TrimSpace(cfg.Agent.SubagentModel) {
			return raw
		}
		target, selected, err := subagentModelEntry(cfg, resolver, parent, ref)
		if err != nil {
			return ""
		}
		if normalized, ok := config.NormalizeInheritedEffort(&target, raw); ok {
			return normalized
		}
		if parent != nil && selected == modelRefFromEntry(parent) {
			return ""
		}
		if _, seen := announced.LoadOrStore(selected, true); !seen {
			report(opts.Sink, event.Event{
				Level:  event.LevelWarn,
				Code:   event.NoticeCodeInheritedSubagentEffortDropped,
				Text:   "Ignored the inherited subagent effort for the selected model.",
				Detail: fmt.Sprintf("agent.subagent_effort = %q is not supported by model %q; that subagent uses the provider/model default effort. The persisted setting was not changed.", raw, selected),
			})
		}
		return ""
	}
}

func newSubagentConfig(opts Options, cfg *config.Config, entry *config.ProviderEntry, modelName string,
	resolver provider.Resolver, proxy netclient.ProxySpec, skills *skill.Store) subagentConfig {
	maxConcurrency, maxWriters := writeclaim.NormalizeConcurrencyLimits(
		cfg.Agent.MaxSubagentConcurrency, cfg.Agent.MaxParallelWriters,
	)
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
						return nil, nil, 0, fmt.Errorf("subagent effort %q on %s: %w", effort, selectedRef, err)
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
		inheritedFor:           inheritedEffortFor(opts, cfg, resolver, entry, &sync.Map{}),
		inheritedEffortDropped: inherited.dropped,
		taskModel:              firstNonEmpty(cfg.Agent.SubagentModels["task"], cfg.Agent.SubagentModel),
		taskEffort:             strings.TrimSpace(cfg.Agent.SubagentEfforts["task"]),
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

// SubagentModelOverride is a subagent_models entry. It outranks the global
// subagent_model for the profile it names, so a surface that shows the global
// value has to name these beside it.
type SubagentModelOverride struct {
	Key   string
	Model string
}

// SubagentModelOverrides lists the non-empty subagent_models entries in key
// order. An empty value overrides nothing and is left out.
func SubagentModelOverrides(cfg *config.Config) []SubagentModelOverride {
	if cfg == nil {
		return nil
	}
	var out []SubagentModelOverride
	for key, model := range cfg.Agent.SubagentModels {
		if m := strings.TrimSpace(model); m != "" {
			out = append(out, SubagentModelOverride{Key: key, Model: m})
		}
	}
	slices.SortFunc(out, func(a, b SubagentModelOverride) int { return strings.Compare(a.Key, b.Key) })
	return out
}

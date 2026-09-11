package boot

import (
	"fmt"
	"slices"
	"strings"

	"reasonix/internal/config"
	"reasonix/internal/extension/providerext"
	"reasonix/internal/provider"
)

// RoleReasoningError retains the adapter error for errors.As while giving every
// frontend enough context to fix the role that actually prevented assembly.
type RoleReasoningError struct {
	Role, Ref, Effort, Source, Kind string
	Supported                       []string
	Err                             error
}

func (e *RoleReasoningError) Error() string {
	return fmt.Sprintf("%s model %q: effort=%q (source=%s, API=%s, supported=%v): %v", e.Role, e.Ref, e.Effort, e.Source, e.Kind, e.Supported, e.Err)
}

func (e *RoleReasoningError) Unwrap() error { return e.Err }

// ValidateReasoningSnapshot lets Desktop validate a pending configuration
// before a workspace correction is allowed to retire its current controller.
func ValidateReasoningSnapshot(cfg *config.Config, opts Options) error {
	return preflightRoleReasoning(cfg, opts, opts.ProviderResolver, false)
}

// resolveBuildConfiguration gives a caller-owned snapshot the runtime contract
// LoadModelRuntimeSnapshot provides: legacy refs expanded, then credentials
// frozen so no lazy resolver rereads the store during the runtime's lifetime.
func resolveBuildConfiguration(root, modelRef string, snapshot *config.Config) (*config.Config, error) {
	if snapshot != nil {
		config.NormalizeLegacyMimoCustomProvidersForRefs(snapshot, modelRef)
		snapshot.FreezeProviderCredentials()
		return snapshot, nil
	}
	return config.LoadModelRuntimeSnapshot(root, modelRef)
}

// explicitVisionModel returns the configured vision reference, or "" when the
// runtime selects one within the active provider ("auto" or unset).
func explicitVisionModel(cfg *config.Config) string {
	ref := strings.TrimSpace(cfg.Agent.VisionModel)
	if strings.EqualFold(ref, "auto") {
		return ""
	}
	return ref
}

// preflightRoleReasoning uses the same immutable config snapshot as assembly.
// The first pass excludes extension refs because their adapter declarations are
// obtained by the extension handshake; the second pass validates those refs
// before any session, workspace lease, MCP tools or controller is constructed.
func preflightRoleReasoning(cfg *config.Config, opts Options, resolver provider.Resolver, extensionsOnly bool) error {
	model := strings.TrimSpace(opts.Model)
	if model == "" {
		model, _, _ = cfg.ResolveNewSessionChatModel()
	}
	type roleSelection struct {
		role, ref, source string
		effort            *string
		// optional roles are constructed lazily at use time, so an unresolvable
		// reference keeps that fallback; only a resolvable one is validated.
		optional bool
	}
	roles := []roleSelection{
		{role: "execution", ref: model, source: "session effort override", effort: opts.EffortOverride},
		{role: "planner", ref: effectivePlannerModel(cfg, opts)},
		{role: "vision", ref: explicitVisionModel(cfg), optional: true},
		{role: "guardian", ref: cfg.Agent.GuardianModel},
		{role: "recovery", ref: cfg.Agent.RecoveryModel},
	}
	subagentModel := strings.TrimSpace(cfg.Agent.SubagentModel)
	if subagentModel == "" {
		subagentModel = model
	}
	var subagentEffort *string
	if cfg.Agent.SubagentEffort != "" {
		value := cfg.Agent.SubagentEffort
		subagentEffort = &value
	}
	if cfg.Agent.SubagentModel != "" || subagentEffort != nil {
		roles = append(roles, roleSelection{role: "subagent", ref: subagentModel, source: "agent.subagent_effort", effort: subagentEffort, optional: true})
	}
	keys := make([]string, 0, len(cfg.Agent.SubagentModels)+len(cfg.Agent.SubagentEfforts))
	for key := range cfg.Agent.SubagentModels {
		keys = append(keys, key)
	}
	for key := range cfg.Agent.SubagentEfforts {
		if !slices.Contains(keys, key) {
			keys = append(keys, key)
		}
	}
	slices.Sort(keys)
	for _, key := range keys {
		ref := strings.TrimSpace(cfg.Agent.SubagentModels[key])
		if ref == "" {
			ref = subagentModel
		}
		effort, source := subagentEffort, "agent.subagent_effort"
		if value := cfg.Agent.SubagentEfforts[key]; value != "" {
			effort, source = &value, "agent.subagent_efforts."+key
		}
		roles = append(roles, roleSelection{role: "subagent[" + key + "]", ref: ref, source: source, effort: effort, optional: true})
	}
	for _, selection := range roles {
		ref := strings.TrimSpace(selection.ref)
		if ref == "" || (providerext.PluginRefOwner(ref) != "") != extensionsOnly {
			continue
		}
		entry, resolved, err := resolveModelEntry(resolver, cfg, ref)
		if err != nil {
			// A migrated account identity must still fail closed, and a
			// configured invalid effort is checked whenever the model resolves.
			if selection.optional && cfg.ModelReferenceError(ref) == nil && !extensionsOnly {
				continue
			}
			return fmt.Errorf("%s_model %q: %w", selection.role, ref, err)
		}
		copy := *entry
		source := "provider default"
		if copy.Effort != "" {
			source = "providers." + copy.Name + ".effort"
		} else if copy.DefaultEffort != "" {
			source = "providers." + copy.Name + ".default_effort"
			if raw, ok := cfg.Provider(copy.Name); ok && raw.ModelOverrides[copy.Model].DefaultEffort != "" {
				source = "providers." + copy.Name + ".model_overrides." + copy.Model + ".default_effort"
			}
		}
		if selection.effort != nil {
			copy.Effort, source = *selection.effort, selection.source
			if copy.Kind == "anthropic" && copy.Effort != "" && copy.Thinking == "" {
				copy.Thinking = "adaptive"
			}
		}
		cap := config.ReasoningCapabilityForEntry(&copy)
		effort := config.EffectiveEffort(&copy)
		if err := cap.Validate(copy.Model, effort); err != nil {
			if effort == "" {
				effort = cap.Default
			}
			return &RoleReasoningError{selection.role, resolved, effort, source, copy.Kind, cap.IDs(), err}
		}
	}
	return nil
}

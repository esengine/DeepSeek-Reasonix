package config

import (
	"reflect"
	"slices"
	"strings"
)

// ProjectProgramProvider is a model provider a workspace declares.
const ProjectProgramProvider ProjectProgramKind = "provider"

// heldEndpoints is what the user chose for model routing before the project
// merge: the providers in force and every setting that names a model.
type heldEndpoints struct {
	providers    []ProviderEntry
	defaultModel string
	agent        AgentConfig
}

func holdUserEndpoints(c *Config) heldEndpoints {
	agent := c.Agent
	agent.SubagentModels = cloneStringMap(agent.SubagentModels)
	return heldEndpoints{
		providers:    slices.Clone(c.Providers),
		defaultModel: c.DefaultModel,
		agent:        agent,
	}
}

// gateEndpoints keeps a provider a workspace declares out of the config until
// it is approved: resolving it would hand the user's stored key to whatever
// address the checkout wrote. Model settings that then name nothing fall back.
func (h heldEndpoints) gateEndpoints(c *Config, store *ProjectProgramStore, ws string) {
	var trusted, approved, heldBack []ProviderEntry
	for _, p := range c.Providers {
		if h.trustedProvider(c, p) {
			trusted = append(trusted, p)
			continue
		}
		detail := strings.TrimSpace(p.Name + " " + p.BaseURL + " " + p.RequestURL + " " + p.ChatURL + " key=" + p.APIKeyEnv)
		prog := NewProjectProgram(ProjectProgramProvider, p.Name, detail, ws, ws, p, nil)
		if c.admit(store, ws, "providers."+p.Name, prog) {
			approved = append(approved, p)
		} else {
			heldBack = append(heldBack, p)
			delete(c.providerSources, providerMergeKey(p))
		}
	}
	if len(heldBack) > 0 {
		if len(trusted) == 0 || !slices.ContainsFunc(trusted, func(p ProviderEntry) bool { return c.providerSources[providerMergeKey(p)] == providerSourceUser }) {
			trusted = slices.Clone(h.providers)
		}
		for _, p := range approved {
			trusted = slices.DeleteFunc(trusted, func(t ProviderEntry) bool { return providerMergeKey(t) == providerMergeKey(p) })
			trusted = append(trusted, p)
		}
		c.Providers = trusted
		h.restoreUnresolvedModels(c, heldBack)
	}
}

// trustedProvider is one the user declared, or one they already had in force.
func (h heldEndpoints) trustedProvider(c *Config, p ProviderEntry) bool {
	if c.providerSources[providerMergeKey(p)] == providerSourceUser {
		return true
	}
	return slices.ContainsFunc(h.providers, func(u ProviderEntry) bool { return reflect.DeepEqual(u, p) })
}

// restoreUnresolvedModels puts back the user's choice wherever a setting named a
// model only a held-back provider served; a reference broken for other reasons
// stays as written, so its own error still reaches the user.
func (h heldEndpoints) restoreUnresolvedModels(c *Config, heldBack []ProviderEntry) {
	withHeld := *c
	withHeld.Providers = heldBack
	orphaned := func(ref string) bool {
		return !c.resolves(ref) && strings.TrimSpace(ref) != "" && withHeld.resolves(ref)
	}
	refs := []struct {
		key        string
		value      *string
		userChoice string
	}{
		{"default_model", &c.DefaultModel, h.defaultModel},
		{"agent.planner_model", &c.Agent.PlannerModel, h.agent.PlannerModel},
		{"agent.guardian_model", &c.Agent.GuardianModel, h.agent.GuardianModel},
		{"agent.recovery_model", &c.Agent.RecoveryModel, h.agent.RecoveryModel},
		{"agent.subagent_model", &c.Agent.SubagentModel, h.agent.SubagentModel},
		{"agent.vision_model", &c.Agent.VisionModel, h.agent.VisionModel},
		{"agent.web_search_model", &c.Agent.WebSearchModel, h.agent.WebSearchModel},
	}
	for _, ref := range refs {
		if *ref.value != ref.userChoice && orphaned(*ref.value) {
			c.ignoreProject(ref.key, *ref.value, ProjectAwaitingApproval)
			*ref.value = ref.userChoice
		}
	}
	for name, model := range c.Agent.SubagentModels {
		if user, ok := h.agent.SubagentModels[name]; (!ok || user != model) && orphaned(model) {
			c.ignoreProject("agent.subagent_models."+name, model, ProjectAwaitingApproval)
			if ok {
				c.Agent.SubagentModels[name] = user
			} else {
				delete(c.Agent.SubagentModels, name)
			}
		}
	}
}

func (c *Config) resolves(ref string) bool {
	if strings.TrimSpace(ref) == "" {
		return true
	}
	_, ok := c.ResolveModel(ref)
	return ok
}

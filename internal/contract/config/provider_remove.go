package config

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// ProviderRemoval says what removing a provider changed besides the provider:
// the roles handed to MovedTo, and the roles that were emptied because nothing
// could take them.
type ProviderRemoval struct {
	MovedTo string
	Moved   []string
	Cleared []string
}

// RemoveProvider deletes the named provider. References to the removed provider
// move to the first remaining configured provider, or clear when none exists;
// roles needing a specific kind of model are cleared, never moved. Its name is
// also dropped from desktop.provider_access.
// ErrProviderNotFound is a removal of a provider the file does not hold.
var ErrProviderNotFound = errors.New("no such provider")

func (c *Config) RemoveProvider(name string) (ProviderRemoval, error) {
	var out ProviderRemoval
	name = strings.TrimSpace(name)
	idx := -1
	for i := range c.Providers {
		if c.Providers[i].Name == name {
			idx = i
			break
		}
	}
	if idx < 0 {
		return out, fmt.Errorf("remove provider %q: %w", name, ErrProviderNotFound)
	}

	defaultRefsProvider := c.modelRefTargetsProvider(c.DefaultModel, name)
	plannerRefsProvider := c.modelRefTargetsProvider(c.Agent.PlannerModel, name)
	subagentRefsProvider := c.modelRefTargetsProvider(c.Agent.SubagentModel, name)
	var skills []string
	for skill, ref := range c.Agent.SubagentModels {
		if c.modelRefTargetsProvider(ref, name) {
			skills = append(skills, skill)
		}
	}
	sort.Strings(skills)

	fallback := ""
	if defaultRefsProvider || plannerRefsProvider || subagentRefsProvider || len(skills) > 0 {
		fallback = c.providerRemovalFallback(name)
	}
	out.Cleared = c.clearUnmovableRoleRefs(name)
	c.Providers = append(c.Providers[:idx], c.Providers[idx+1:]...)
	c.dropProviderAccess(name)

	record := func(role string) {
		if fallback != "" {
			out.Moved = append(out.Moved, role)
			out.MovedTo = fallback
		} else {
			out.Cleared = append(out.Cleared, role)
		}
	}
	if defaultRefsProvider {
		c.DefaultModel = fallback
		record("default")
	}
	if plannerRefsProvider {
		c.Agent.PlannerModel = fallback
		record("planner")
	}
	if subagentRefsProvider {
		c.Agent.SubagentModel = fallback
		record("subagent")
	}
	for _, skill := range skills {
		if fallback != "" {
			c.Agent.SubagentModels[skill] = fallback
		} else {
			delete(c.Agent.SubagentModels, skill)
		}
		record("subagent:" + skill)
	}
	return out, nil
}

// dropProviderAccess removes name from desktop.provider_access, keeping a
// declared list declared: an emptied one still means "none shown", not "all".
func (c *Config) dropProviderAccess(name string) {
	if c.Desktop.ProviderAccess == nil {
		return
	}
	kept := make([]string, 0, len(c.Desktop.ProviderAccess))
	for _, n := range c.Desktop.ProviderAccess {
		if strings.TrimSpace(n) != name {
			kept = append(kept, n)
		}
	}
	c.Desktop.ProviderAccess = kept
}

func (c *Config) providerRemovalFallback(name string) string {
	for i := range c.Providers {
		p := &c.Providers[i]
		if p.Name == name || !p.Configured() || len(p.ModelList()) == 0 {
			continue
		}
		return p.Name
	}
	return ""
}

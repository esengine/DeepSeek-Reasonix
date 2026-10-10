// roles.go — the model refs a role assignment can name.
package config

// roleModelRef is one field a role points a model at, under the name the
// person sees it by.
type roleModelRef struct {
	Role string
	Ref  *string
}

// roleModelRefTargets lists every field a role points a model at. The fallback
// repair, the referenced-provider scan and the retired-model rename all walk
// this set, and they have to walk the same one: a role missing from any of them
// keeps a ref alive that no provider can resolve.
func (c *Config) roleModelRefTargets() []*string {
	named := c.namedRoleModelRefs()
	out := make([]*string, len(named))
	for i, r := range named {
		out[i] = r.Ref
	}
	return out
}

func (c *Config) namedRoleModelRefs() []roleModelRef {
	if c == nil {
		return nil
	}
	return []roleModelRef{
		{"planner", &c.Agent.PlannerModel}, {"subagent", &c.Agent.SubagentModel},
		{"vision", &c.Agent.VisionModel}, {"guardian", &c.Agent.GuardianModel},
		{"recovery", &c.Agent.RecoveryModel}, {"triage", &c.Agent.TriageModel},
		{"decision", &c.Agent.DecisionModel}, {"advisor", &c.Agent.AdvisorModel},
	}
}

func (c *Config) roleModelRefs() []string {
	targets := c.roleModelRefTargets()
	out := make([]string, 0, len(targets))
	for _, ref := range targets {
		out = append(out, *ref)
	}
	return out
}

// clearUnmovableRoleRefs empties the roles naming provider that cannot be handed
// to an arbitrary chat model: vision needs image input, decision a decision
// wire, advisor a different model than the main one. It returns the roles it
// emptied.
func (c *Config) clearUnmovableRoleRefs(provider string) []string {
	var cleared []string
	for _, r := range c.namedRoleModelRefs() {
		if r.Role == "planner" || r.Role == "subagent" {
			continue
		}
		if c.modelRefTargetsProvider(*r.Ref, provider) {
			*r.Ref = ""
			cleared = append(cleared, r.Role)
		}
	}
	return cleared
}

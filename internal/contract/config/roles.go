// roles.go — the model refs a role assignment can name.
package config

// roleModelRefTargets lists every field a role points a model at. The fallback
// repair, the referenced-provider scan and the retired-model rename all walk
// this set, and they have to walk the same one: a role missing from any of them
// keeps a ref alive that no provider can resolve.
func (c *Config) roleModelRefTargets() []*string {
	if c == nil {
		return nil
	}
	return []*string{
		&c.Agent.PlannerModel, &c.Agent.SubagentModel, &c.Agent.VisionModel,
		&c.Agent.GuardianModel, &c.Agent.RecoveryModel, &c.Agent.TriageModel,
		&c.Agent.DecisionModel, &c.Agent.AdvisorModel,
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

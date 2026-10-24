package control

import "reasonix/internal/contract/config"

// SubagentClaimsSettings is how a sub-agent that declared no write_paths holds
// its claim, as the user file records it. It is the user's alone: a project
// file cannot set it, so there is no second effective value to show.
type SubagentClaimsSettings struct {
	Optimistic bool   `json:"optimistic"`
	Path       string `json:"path"`
}

// SubagentClaimsSettings reads [agent] subagent_claims from the user file.
func (c *Controller) SubagentClaimsSettings() SubagentClaimsSettings {
	path := c.writeCoordinationConfigPath()
	return SubagentClaimsSettings{Optimistic: config.LoadForEdit(path).SubagentClaimsOptimistic(), Path: path}
}

// SaveSubagentClaimsSettings persists the choice to the user file. The caller
// rebuilds: boot hands the claim policy to the delegation tools while
// assembling, so a live runtime keeps the policy it was built with.
func (c *Controller) SaveSubagentClaimsSettings(optimistic bool) error {
	return c.editWriteCoordination(func(cfg *config.Config) error {
		cfg.SetSubagentClaims(optimistic)
		return nil
	})
}

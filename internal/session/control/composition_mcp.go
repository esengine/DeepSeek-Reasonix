package control

import (
	"encoding/json"
	"fmt"

	"reasonix/internal/contract/agentcomposition"
	"reasonix/internal/contract/config"
	"reasonix/internal/state/agentpreset"
)

func (c *Controller) pinnedMCPEntry(live config.PluginEntry) (config.PluginEntry, error) {
	binding := c.CompositionBinding()
	if binding == nil {
		return live, nil
	}
	for _, r := range binding.Resources {
		if r.Kind != "mcp" || r.ID != live.Name {
			continue
		}
		var snapshot agentpreset.MCPSnapshot
		if err := json.Unmarshal(r.Data, &snapshot); err != nil {
			return live, err
		}
		saved, err := snapshot.Resolve(live)
		if err != nil {
			return live, err
		}
		for _, m := range binding.Resolved.MCP.Members {
			if m.ID != live.Name {
				continue
			}
			p, err := agentcomposition.ParseParameters(m.Parameters)
			if err != nil {
				return live, err
			}
			if p.Env != nil {
				saved.Env = p.Env
			}
			if p.Headers != nil {
				saved.Headers = p.Headers
			}
		}
		return saved, nil
	}
	return live, fmt.Errorf("%w: MCP %s is outside this composition", agentcomposition.ErrLocked, live.Name)
}

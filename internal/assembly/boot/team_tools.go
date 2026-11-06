package boot

import (
	"slices"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/tool"
	"reasonix/internal/tools/builtin"
)

// teamTools registers the team tools in a session of an agent on the user's
// team, and nowhere else, so every other session's tool block is unchanged.
// Every agent session gets the same set: what one may do is decided per call.
func teamTools(reg *tool.Registry, cfg *config.Config, seated bool) *tool.TeamLink {
	if !seated {
		return nil
	}
	link := &tool.TeamLink{}
	for _, t := range builtin.TeamTools(link) {
		if len(cfg.Tools.Enabled) == 0 || slices.Contains(cfg.Tools.Enabled, t.Name()) {
			reg.Add(t)
		}
	}
	return link
}

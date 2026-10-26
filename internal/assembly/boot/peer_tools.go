package boot

import (
	"slices"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/tool"
	"reasonix/internal/tools/builtin"
)

// peerToolNames are the tools the peer switches own, so a switch turned off
// withdraws exactly these.
var peerToolNames = []string{"list_peers", "send_peer_message", "dispatch_to_project", "open_projects"}

// peerTools registers the tools that let the model see and message other
// sessions, and only while the user has the switch on, so the tool block of
// every other session is unchanged. The returned function moves the set when the
// user flips a switch; the controller calls it only between turns.
func peerTools(reg *tool.Registry, cfg *config.Config) (*tool.PeerLink, func(enabled, dispatch bool)) {
	link := &tool.PeerLink{}
	allowed := func(name string) bool {
		return len(cfg.Tools.Enabled) == 0 || slices.Contains(cfg.Tools.Enabled, name)
	}
	set := func(enabled, dispatch bool) {
		reg.ShowToProvider(false, peerToolNames...)
		for _, name := range peerToolNames {
			reg.RemovePrefix(name)
		}
		if !enabled {
			return
		}
		add := builtin.PeerTools(link)
		if dispatch {
			add = append(add, builtin.DispatchTool(link), builtin.OpenProjectsTool(link))
		}
		for _, t := range add {
			if allowed(t.Name()) {
				reg.Add(t)
				reg.ShowToProvider(true, t.Name())
			}
		}
	}
	set(cfg.PeersEnabled(), cfg.PeersDispatch())
	return link, set
}

package boot

import (
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/tool"
	"reasonix/internal/session/control"
)

// A disabled session keeps its spool location on standby so the user can
// enable messaging between turns without rebuilding the controller.
func peersOptions(cfg *config.Config, link *tool.PeerLink, tools func(enabled, dispatch bool)) control.PeersOptions {
	return control.PeersOptions{
		Dir: cfg.Roots().PeersDir(), Link: link, Tools: tools,
		Standby: !cfg.PeersEnabled(), Dispatch: cfg.PeersDispatch(),
	}
}

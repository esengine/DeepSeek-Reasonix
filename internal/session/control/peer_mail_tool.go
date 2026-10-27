package control

import (
	"errors"
	"fmt"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/settings"
	"reasonix/internal/contract/tool"
)

// peerDirectory is the controller as the peer tools see it. A refusal reaches
// the model as its identity first, so it can tell "no such session" from "slow
// down" without reading a sentence.
type peerDirectory struct{ c *Controller }

func (d peerDirectory) Peers() []tool.PeerView {
	var out []tool.PeerView
	for _, p := range d.c.Peers() {
		out = append(out, tool.PeerView{Key: p.Key, Title: p.Title, Project: p.Project})
	}
	return out
}

func (d peerDirectory) SendPeerMessage(to, text string) (tool.PeerReceipt, error) {
	r, err := d.c.SendPeerMessage(to, text)
	if err != nil {
		return tool.PeerReceipt{}, fmt.Errorf("%s: %w", peerErrorCode(err), err)
	}
	return tool.PeerReceipt{ID: r.ID, To: tool.PeerView{Key: r.To.Key, Title: r.To.Title, Project: r.To.Project}}, nil
}

// fmtPeerError is the refusal a peer tool reports: the error's identity, and
// the setting whose change would lift it where one would.
func fmtPeerError(err error) error {
	code := peerErrorCode(err)
	return tool.Refusal{Code: code, Message: err.Error(), Remedy: peerRemedy(code)}
}

// peerRemedy points at the switch a refusal names: messages between sessions
// when they are off, else the dispatch switch.
func peerRemedy(code string) *tool.Remedy {
	switch code {
	case "peer.disabled":
		return settings.RemedyFor("peers.enabled", true)
	case "dispatch.disabled":
		if !config.UserPeers().Enabled {
			return settings.RemedyFor("peers.enabled", true)
		}
		return settings.RemedyFor("peers.dispatch", true)
	}
	return nil
}

func peerErrorCode(err error) string {
	for code, sentinel := range map[string]error{
		"peer.disabled": ErrPeersDisabled, "peer.not_found": ErrPeerNotFound, "peer.ambiguous": ErrPeerAmbiguous,
		"peer.self": ErrPeerSelf, "peer.refused": ErrPeerRefused, "peer.rate_limited": ErrPeerRateLimited,
		"peer.empty": ErrPeerEmpty, "peer.too_large": ErrPeerTooLarge,
		"dispatch.disabled": ErrDispatchDisabled, "dispatch.unavailable": ErrDispatchUnavailable, "dispatch.not_local": ErrDispatchNotLocal, "dispatch.unknown_project": ErrDispatchUnknownProject,
		"dispatch.ambiguous": ErrDispatchAmbiguous, "dispatch.nested": ErrDispatchNested,
		"dispatch.untrusted": ErrDispatchUntrusted, "dispatch.awaiting_trust": ErrDispatchAwaitingTrust, "dispatch.same_project": ErrDispatchSameProject, "dispatch.failed": ErrDispatchFailed, "dispatch.limit": ErrDispatchLimit,
		"open_projects.empty": ErrOpenProjectsEmpty, "open_projects.too_many": ErrOpenProjectsTooMany, "open_projects.relative": ErrOpenProjectsRelative,
		"open_projects.declined": ErrOpenProjectsDeclined, "open_projects.no_answer": ErrOpenProjectsNoAnswer, "open_projects.plan_mode": ErrOpenProjectsPlan,
	} {
		if errors.Is(err, sentinel) {
			return code
		}
	}
	return "peer.send_failed"
}

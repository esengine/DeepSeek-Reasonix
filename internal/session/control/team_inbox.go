package control

import (
	"reasonix/internal/contract/tool"
	"reasonix/internal/session/team"
	"reasonix/internal/state/sessioninbox"
)

var errTeamWorkSettled = &tool.Refusal{Code: "team.interrupted", Message: "This queued team task was already settled and will not be replayed."}

// Delivery can synchronously admit an inbox item while holding the group lock.
// Check only in the admitted turn body, after delivery can publish its round.
func (c *Controller) checkTeamInbox(env sessioninbox.PromptEnvelope, origin sessioninbox.PromptOrigin) error {
	kind := team.DeliveryKind(env.Extra[team.ExtraKind])
	if !origin.IsHost() || (kind != team.DeliverMessage && kind != team.DeliverGroupTurn) {
		return nil
	}
	seat := c.team.bound()
	if seat == nil {
		return errTeamWorkSettled
	}
	svc, ref := seat.Team()
	if svc == nil {
		return errTeamWorkSettled
	}
	var pending bool
	var err error
	if kind == team.DeliverMessage {
		pending, err = svc.MessagePending(ref, env.Extra[team.ExtraExchange])
	} else {
		pending, err = svc.GroupTurnPending(ref, env.Extra[team.ExtraStep])
	}
	if err != nil {
		return err
	}
	if !pending {
		return errTeamWorkSettled
	}
	return nil
}

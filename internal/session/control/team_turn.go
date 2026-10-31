package control

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"

	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/session/team"
	"reasonix/internal/state/sessioninbox"
)

// DeliverTeam queues a team delivery in this session's inbox as a turn the
// host starts, recording what it is so the turn that runs it reports back.
// The delivery's key makes a retry after a crash land once.
func (c *Controller) DeliverTeam(d team.Delivery) error {
	if err := team.CheckPlatform(); err != nil {
		return teamRefusal(err)
	}
	extra := map[string]string{team.ExtraKind: string(d.Kind)}
	for k, v := range map[string]string{
		team.ExtraFrom: d.From, team.ExtraExchange: d.Exchange, team.ExtraServing: strings.Join(d.Serving, ","),
		team.ExtraStep: d.Step, team.ExtraGroup: d.Group, team.ExtraCeiling: d.Ceiling,
	} {
		if v != "" {
			extra[k] = v
		}
	}
	req := InboxRequest{
		Origin: sessioninbox.OriginHost, Source: "team:" + d.From, Idempotency: d.Key,
		Display: d.Display, Raw: d.Display, Submit: d.Text, Extra: extra,
	}
	var err error
	if d.Steer && stricterApproval(c.effectiveApprovalMode(), d.Ceiling) == c.effectiveApprovalMode() {
		_, err = c.TryEnqueueAndSteer(req)
	} else {
		_, err = c.TryEnqueueFollowup(req)
	}
	return err
}

// teamOrigin is the origin a team delivery's Extra declares, nil for an item
// the team did not queue.
func teamOrigin(extra map[string]string) *provider.Origin {
	kind := extra[team.ExtraKind]
	if kind == "" {
		return nil
	}
	return &provider.Origin{Kind: kind, Agent: extra[team.ExtraFrom], Exchange: extra[team.ExtraExchange], Group: extra[team.ExtraGroup]}
}

// turnFinalText is the answer this turn ended with: the last assistant text
// after the turn's own input. A turn that wrote none has none, rather than the
// previous turn's.
func turnFinalText(msgs []provider.Message) string {
	for _, m := range slices.Backward(msgs) {
		switch {
		case m.Role == provider.RoleUser:
			return ""
		case m.Role == provider.RoleAssistant && strings.TrimSpace(m.Content) != "":
			return m.Content
		}
	}
	return ""
}

// turnFailureCode is the host's identity for why a turn failed.
func turnFailureCode(err error) string {
	var identity tool.RefusalIdentity
	switch {
	case errors.As(err, &identity) && identity.RefusalCode() != "":
		return identity.RefusalCode()
	case errors.Is(err, context.DeadlineExceeded):
		return "turn.timeout"
	}
	var readiness *agent.FinalReadinessError
	if errors.As(err, &readiness) {
		return "turn.final_readiness"
	}
	if _, ok := agent.InspectRunPause(err); ok {
		return "turn.paused"
	}
	return "turn.failed"
}

// settleTeamTurn reports a finished turn to the team, off the turn's own path:
// what it settles may queue new turns, here or in another agent's session.
func (c *Controller) settleTeamTurn(t teamTurn, err error, cancelled bool) {
	if errors.Is(err, errTeamWorkSettled) {
		return
	}
	seat := c.team.bound()
	if seat == nil || (len(t.serving) == 0 && len(t.consumed) == 0 && len(t.steps) == 0) {
		return
	}
	svc, ref := seat.Team()
	if svc == nil {
		return
	}
	st := team.Settlement{Serving: t.serving, Consumed: t.consumed, Steps: t.steps, Outcome: team.OutcomeCompleted}
	switch {
	case cancelled:
		st.Outcome = team.OutcomeCancelled
	case err != nil:
		st.Outcome, st.Code = team.OutcomeFailed, turnFailureCode(err)
	default:
		st.FinalText = turnFinalText(c.History())
	}
	go func() {
		if err := svc.Settled(context.Background(), ref, st); err != nil {
			slog.Warn("controller: settle a team turn", "err", err)
		}
	}()
}

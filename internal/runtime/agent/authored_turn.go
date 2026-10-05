package agent

import (
	"context"
	"fmt"
	"reasonix/internal/state/sessionstore"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
)

// PriorAuthoredTurn is the authored-turn count over msgs: the number the next
// message appended after them would follow. Re-derived from the transcript
// rather than tracked beside it, so no counter can disagree with the messages
// it claims to number.
func PriorAuthoredTurn(msgs []provider.Message) int {
	turn := 0
	for _, m := range msgs {
		turn = sessionstore.ClassifyTurn(m, turn).AuthoredTurn
	}
	return turn
}

// HostTurnBoundary declares that the host announced this run's turn boundary,
// so the run announces none of its own. Authored is the identity it published,
// nil when the run continues a turn already announced. HostAuthored is a
// separate fact: a turn can open none and still be the user's, because their
// line only has to read like one the host writes.
type HostTurnBoundary struct {
	Authored     *sessionstore.AuthoredTurnIdentity
	HostAuthored bool
	// Via is the paired device the message was sent from, nil for the window.
	Via *provider.Via
}

type hostTurnBoundaryKey struct{}

// WithHostTurnBoundary hands a run the boundary its host owns. Ownership is
// declared, never observed: a run must not decide it has been announced
// because some event already reached the sink.
func WithHostTurnBoundary(ctx context.Context, boundary HostTurnBoundary) context.Context {
	return context.WithValue(ctx, hostTurnBoundaryKey{}, boundary)
}

// HostTurnBoundaryFrom returns the host's boundary, if a host took it.
func HostTurnBoundaryFrom(ctx context.Context) (HostTurnBoundary, bool) {
	if ctx == nil {
		return HostTurnBoundary{}, false
	}
	boundary, ok := ctx.Value(hostTurnBoundaryKey{}).(HostTurnBoundary)
	return boundary, ok
}

// LandAuthoredUserMessage appends the message this turn is about, holding it to
// the identity the host published for it: the host owns the identity, whoever
// writes the message owns its Content. Exported because a host that persists a
// turn's message itself has to land it through this seam too — a second place
// that appends is a second answer to which message the turn was about.
func (a *Agent) LandAuthoredUserMessage(ctx context.Context, msg provider.Message) {
	if a == nil || a.sess.conversation == nil {
		return
	}
	if a.svc.admissionSaver != nil {
		defer a.svc.admissionSaver.SaveAdmittedMessage(ctx)
	}
	boundary, hosted := HostTurnBoundaryFrom(ctx)
	if !hosted {
		a.sess.conversation.Add(msg)
		return
	}
	if boundary.Authored != nil && boundary.Authored.Raw != "" {
		msg.RawContent = boundary.Authored.Raw
	}
	msg.HostAuthored = boundary.HostAuthored
	msg.Via = boundary.Via
	index := a.sess.conversation.AddIndexed(msg)
	if boundary.Authored != nil {
		a.verifyAuthoredLanding(*boundary.Authored, msg, index)
	}
}

// announceOwnTurn opens a turn boundary for a run no host announced one for.
// A standalone Run is its own lifecycle owner and still owes its sink a start;
// under a host boundary it owes nothing, because the host already said it.
func (a *Agent) announceOwnTurn(ctx context.Context, pending provider.Message) {
	if _, hosted := HostTurnBoundaryFrom(ctx); hosted {
		return
	}
	if started, ok := a.turnStartedEvent(pending); ok {
		a.svc.sink.Emit(started)
		return
	}
	a.svc.sink.Emit(event.Event{Kind: event.TurnStarted, ModelRef: a.modelRef})
}

// verifyAuthoredLanding checks the published identity against the message that
// actually landed. It detects; it cannot repair — the name is already out — so
// a break is reported to the operator rather than swallowed, and the durable
// index remains the authority a reload reads.
func (a *Agent) verifyAuthoredLanding(id sessionstore.AuthoredTurnIdentity, msg provider.Message, index int) {
	class := sessionstore.ClassifyTurn(msg, id.AuthoredTurn-1)
	if index == id.MsgIndex && class.StartsTurn && class.AuthoredTurn == id.AuthoredTurn {
		return
	}
	a.svc.sink.Emit(event.Event{
		Kind: event.Notice, Level: event.LevelWarn, Audience: event.NoticeAudienceOperator,
		Code: event.NoticeCodeTurnIdentityMismatch,
		Text: "the announced turn does not name the message that landed",
		Detail: fmt.Sprintf("announced turn %d at message %d; landed at %d as turn %d (starts_turn=%v)",
			id.AuthoredTurn, id.MsgIndex, index, class.AuthoredTurn, class.StartsTurn),
	})
}

// turnStartedEvent announces the turn and names the message it is about: the
// authored turn that message opens, and the session index it will take. Both
// come from ClassifyTurn against the live transcript, so this projection and
// the durable display index answer the same number for the same message. A
// turn that opens no authored message names none rather than minting one.
func (a *Agent) turnStartedEvent(pending provider.Message) (event.Event, bool) {
	msgs := a.sess.conversation.Snapshot()
	class := sessionstore.ClassifyTurn(pending, PriorAuthoredTurn(msgs))
	if !class.StartsTurn {
		return event.Event{}, false
	}
	index := len(msgs)
	return event.Event{Kind: event.TurnStarted, AuthoredTurn: &class.AuthoredTurn, MsgIndex: &index, ModelRef: a.modelRef}, true
}

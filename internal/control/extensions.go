package control

import (
	"context"
	"errors"
	"log/slog"
	"sync"

	"reasonix/internal/event"
	"reasonix/internal/eventwire"
	"reasonix/internal/extension"
	"reasonix/internal/extension/dispatch"
)

// Extension dispatch wiring (Extension Protocol v1, stage 6b1). The
// dispatcher is the frozen, immutable view of the installed extensions for
// one controller generation; every wiring point here is nil-safe: with no
// dispatcher installed (no v1 runtime packages, or a degraded snapshot) each
// helper returns immediately and the controller behaves byte-identically to
// the pre-dispatch path.
//
// Session payload surface: SessionPayload carries only the session transcript
// path and the phase. Transcript path and metadata decisions stay host-side —
// a session_policy owner's replace ruling adjusts the payload observing
// extensions receive, never which file the host writes, loads, or rotates to.

// extensionSessionEvent broadcasts one session.* point to observing
// extensions, fire-and-forget. session.start and session.end are
// observation-only: no strategy runs at them.
func (c *Controller) extensionSessionEvent(point extension.InterceptorPoint, phase, path string) {
	c.extensionSessionPayloadEvent(point, dispatch.SessionPayload{SessionPath: path, Phase: phase})
}

// interceptInputReceive runs one composed input text through the extension
// chain at input.receive before it enters the session. The returned text is
// what the turn must use (a replace ruling rewrites it); blocked reports an
// extension's block ruling, with the redacted reason already surfaced as a
// user-visible notice by this helper. A required-class extension failure is
// returned as the error. With no dispatcher installed the input passes
// through untouched.
func (c *Controller) interceptInputReceive(ctx context.Context, input string) (text string, blocked bool, err error) {
	if c.extensions == nil {
		return input, false, nil
	}
	payload := dispatch.InputPayload{Text: input}
	result, err := c.extensions.Intercept(ctx, extension.PointInputReceive, &payload)
	if err != nil {
		return input, false, err
	}
	if result.Blocked {
		c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn, Text: "Turn blocked by an extension.", Detail: result.BlockReason})
		return input, true, nil
	}
	return payload.Text, false, nil
}

// extensionSessionPayloadEvent broadcasts one session.* point with an
// already-settled (possibly owner-adjusted) payload.
func (c *Controller) extensionSessionPayloadEvent(point extension.InterceptorPoint, payload dispatch.SessionPayload) {
	if c.extensions == nil {
		return
	}
	c.extensions.Event(point, payload)
}

// extensionSessionStrategy runs only the strategy half of a session.* point
// and returns the final payload for a later event broadcast. Callers that
// must separate the ruling from the observation (session.save rules on the
// impending write but observes the completed one) use this half directly.
func (c *Controller) extensionSessionStrategy(ctx context.Context, point extension.InterceptorPoint, phase, path string) (dispatch.SessionPayload, error) {
	payload := dispatch.SessionPayload{SessionPath: path, Phase: phase}
	if c.extensions == nil {
		return payload, nil
	}
	if _, owned := c.extensions.Strategy(extension.SlotSessionPolicy); owned {
		if err := c.extensions.RunStrategy(ctx, extension.SlotSessionPolicy, point, &payload); err != nil {
			return payload, err
		}
	}
	return payload, nil
}

// extensionSessionPhase runs the session_policy strategy at one session.*
// point (load/save/rotate) when the slot has an owner, then broadcasts the
// event with the final (possibly owner-adjusted) payload. The strategy error
// is returned to the caller: at session.save and session.rotate it is fatal
// to the operation (the owner is required-class by definition); at
// session.load the caller degrades to a warning because Controller.Resume has
// no failure channel this stage.
func (c *Controller) extensionSessionPhase(ctx context.Context, point extension.InterceptorPoint, phase, path string) error {
	if c.extensions == nil {
		return nil
	}
	payload, err := c.extensionSessionStrategy(ctx, point, phase, path)
	if err != nil {
		return err
	}
	c.extensionSessionPayloadEvent(point, payload)
	return nil
}

// extensionWarn surfaces a required-class extension failure to the user and
// the log. It goes through the ordinary sink (the failure itself is a
// frontend event like any other).
func (c *Controller) extensionWarn(what string, err error) {
	slog.Warn("controller: extension "+what, "err", err)
	c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn, Text: "Extension " + what + ": " + err.Error()})
}

// frontendEventSink wraps the controller's sink when a dispatcher is
// installed: every event is observed at frontend.event (fire-and-forget)
// and, when the frontend_events slot has an owner, ruled on first — a
// replacement may rewrite Text/Detail but never the Kind.
type frontendEventSink struct {
	inner event.Sink
	d     *dispatch.Dispatcher

	warnMu sync.Mutex
	warned map[string]bool
}

func newFrontendEventSink(inner event.Sink, d *dispatch.Dispatcher) *frontendEventSink {
	return &frontendEventSink{inner: inner, d: d, warned: map[string]bool{}}
}

// Emit observes and rules on one controller event before forwarding it. The
// strategy runs synchronously (the frontend_events owner is required-class):
// an explicit block ruling suppresses the event, but an owner malfunction
// (timeout, crash, contract violation) emits the original event with a
// warning — dropping ApprovalRequest/TurnDone-class events would hang the
// frontend's state machine, so only an affirmative block ruling may.
func (s *frontendEventSink) Emit(ev event.Event) {
	name, ok := eventwire.KindName(ev.Kind)
	if !ok {
		s.inner.Emit(ev)
		return
	}
	payload := dispatch.FrontendEventPayload{Kind: name, Text: ev.Text, Detail: ev.Detail}
	if _, owned := s.d.Strategy(extension.SlotFrontendEvents); owned {
		before := payload
		if err := s.d.RunStrategy(context.Background(), extension.SlotFrontendEvents, extension.PointFrontendEvent, &payload); err != nil {
			var blockErr *dispatch.BlockError
			if errors.As(err, &blockErr) {
				s.warnOnce("block|"+blockErr.Plugin, "extension frontend event suppressed: "+blockErr.Error())
				return
			}
			s.warnOnce("failure|"+extensionFailurePlugin(err), "extension frontend event strategy failed; emitting the original event: "+err.Error())
			payload = before
		} else if payload.Kind != before.Kind {
			s.warnOnce("kind", "extension frontend event strategy tried to change the event kind; emitting the original event")
			payload = before
		}
	}
	// Observers see exactly what the frontend is about to receive.
	s.d.Event(extension.PointFrontendEvent, payload)
	ev.Text, ev.Detail = payload.Text, payload.Detail
	s.inner.Emit(ev)
}

// warnOnce logs msg at most once per key for the life of the sink. Warnings
// stay on the log: routing them back through the sink would re-enter the
// strategy path they describe.
func (s *frontendEventSink) warnOnce(key, msg string) {
	s.warnMu.Lock()
	if s.warned[key] {
		s.warnMu.Unlock()
		return
	}
	s.warned[key] = true
	s.warnMu.Unlock()
	slog.Warn("controller: " + msg)
}

// extensionFailurePlugin extracts the plugin ID from a dispatch error for
// warn-once keying.
func extensionFailurePlugin(err error) string {
	var failureErr *dispatch.FailureError
	if errors.As(err, &failureErr) {
		return failureErr.Plugin
	}
	var violationErr *dispatch.ViolationError
	if errors.As(err, &violationErr) {
		return violationErr.Plugin
	}
	return "unknown"
}

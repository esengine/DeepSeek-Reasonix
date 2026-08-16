package control

import (
	"context"
	"errors"
	"log/slog"
	"sync"

	"reasonix/internal/event"
	"reasonix/internal/eventwire"
	"reasonix/internal/evidence"
	"reasonix/internal/extension"
	"reasonix/internal/extension/dispatch"
	"reasonix/internal/sessioninbox"
)

// Extension dispatch wiring (stage 6b1). Nil dispatcher is a no-op.
// SessionPayload carries only path + phase; the host owns file decisions.

// extensionSessionEvent broadcasts one session.* point fire-and-forget.
func (c *Controller) extensionSessionEvent(point extension.InterceptorPoint, phase, path string) {
	c.extensionSessionPayloadEvent(point, dispatch.SessionPayload{SessionPath: path, Phase: phase})
}

// loadExtensions returns the dispatcher under c.mu (safe vs ReplaceExtensions).
func (c *Controller) loadExtensions() *dispatch.Dispatcher {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	d := c.extensions
	c.mu.Unlock()
	return d
}

// interceptInputReceive runs input.receive; blocked surfaces a notice.
func (c *Controller) interceptInputReceive(ctx context.Context, input string) (text string, blocked bool, err error) {
	d := c.loadExtensions()
	if d == nil {
		return input, false, nil
	}
	payload := dispatch.InputPayload{Text: input}
	result, err := d.Intercept(ctx, extension.PointInputReceive, &payload)
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
	d := c.loadExtensions()
	if d == nil {
		return
	}
	d.Event(point, payload)
}

// extensionSessionStrategy runs only the strategy half of a session.* point
// and returns the final payload for a later event broadcast. Callers that
// must separate the ruling from the observation (session.save rules on the
// impending write but observes the completed one) use this half directly.
func (c *Controller) extensionSessionStrategy(ctx context.Context, point extension.InterceptorPoint, phase, path string) (dispatch.SessionPayload, error) {
	payload := dispatch.SessionPayload{SessionPath: path, Phase: phase}
	d := c.loadExtensions()
	if d == nil {
		return payload, nil
	}
	if _, owned := d.Strategy(extension.SlotSessionPolicy); owned {
		if err := d.RunStrategy(ctx, extension.SlotSessionPolicy, point, &payload); err != nil {
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
	if c.loadExtensions() == nil {
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

func (s *frontendEventSink) setDispatcher(d *dispatch.Dispatcher) {
	if s == nil || d == nil {
		return
	}
	s.warnMu.Lock()
	s.d = d
	s.warnMu.Unlock()
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
	s.warnMu.Lock()
	d := s.d
	s.warnMu.Unlock()
	payload := dispatch.FrontendEventPayload{Kind: name, Text: ev.Text, Detail: ev.Detail}
	if _, owned := d.Strategy(extension.SlotFrontendEvents); owned {
		before := payload
		if err := d.RunStrategy(context.Background(), extension.SlotFrontendEvents, extension.PointFrontendEvent, &payload); err != nil {
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

func (s *frontendEventSink) InboxChanged(snap sessioninbox.InboxSnapshot) {
	if s == nil {
		return
	}
	notifyInboxChanged(s.inner, snap)
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

// The audit capabilities pass through untouched: extension rulings apply to
// user-facing events, never to the content-free telemetry channels — without
// these, enabling extensions severed every audit from the recorder.

func (s *frontendEventSink) RecordReadinessAudit(a evidence.ReadinessAudit) {
	event.RecordReadinessAudit(s.inner, a)
}

func (s *frontendEventSink) RecordContractShadow(a event.ContractShadowAudit) {
	event.RecordContractShadow(s.inner, a)
}

func (s *frontendEventSink) RecordDelegationAudit(a evidence.DelegationAudit) {
	event.RecordDelegationAudit(s.inner, a)
}

func (s *frontendEventSink) RecordCompletionReport(a event.CompletionReportAudit) {
	event.RecordCompletionReport(s.inner, a)
}

func (s *frontendEventSink) RecordOutcomeProgress(sample evidence.OutcomeSample) {
	event.RecordOutcomeProgress(s.inner, sample)
}

func (s *frontendEventSink) RecordDelegationAdmission(a event.DelegationAdmissionAudit) {
	event.RecordDelegationAdmission(s.inner, a)
}

func (s *frontendEventSink) RecordMemoryRecall(a event.MemoryRecallAudit) {
	event.RecordMemoryRecall(s.inner, a)
}

func (s *frontendEventSink) RecordProtocolRecovery(a event.ProtocolRecoveryAudit) {
	event.RecordProtocolRecovery(s.inner, a)
}

func (s *frontendEventSink) RecordTurnCompletion() {
	event.RecordTurnCompletion(s.inner)
}

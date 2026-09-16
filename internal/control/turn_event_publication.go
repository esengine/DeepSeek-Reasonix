package control

import (
	"reasonix/internal/event"
	"reasonix/internal/eventwire"
	"reasonix/internal/turnevent"
	"time"
)

func (s *turnEventSink) publishOutsideTurn(ledger *turnevent.Ledger, e event.Event) error {
	if ledger.CurrentStatus() == event.TurnRecoveryRequired && lateBusinessEvent(e.Kind) {
		return nil
	}
	s.c.refreshRuntimeState(e)
	if _, runtime, exclusive := s.c.v3Binding(); exclusive && runtime != nil {
		wire := eventwire.ToWire(e)
		if err := runtime.PublishTranscriptFrame(turnevent.Envelope{Kind: wire.Kind, Event: wire, CreatedAt: time.Now().UnixMilli()}); err != nil {
			return err
		}
	}
	s.publishInner(e)
	return nil
}

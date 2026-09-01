package main

import "reasonix/internal/event"

// RecordCompletionValidation consumes host-only completion validation telemetry
// without publishing it to the frontend, Serve, bot, or persisted event wire.
// Persist immediately so a background sub-agent does not depend on the root
// turn eventually emitting TurnDone.
func (s *tabEventSink) RecordCompletionValidation(info event.CompletionValidationInfo) {
	if s == nil {
		return
	}
	_, app := s.binding()
	if app == nil {
		return
	}
	if metrics := app.metrics.Load(); metrics != nil {
		metrics.observeCompletionValidation(info)
		metrics.persist()
	}
}

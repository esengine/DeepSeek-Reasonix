package main

import "reasonix/internal/event"

// RecordSubagentLifecycle consumes content-free child lifecycle telemetry.
// ToolResult already carries the live card metadata; this side channel only
// contributes scrubbed status/error buckets to anonymous desktop diagnostics.
func (s *tabEventSink) RecordSubagentLifecycle(info event.SubagentLifecycleInfo) {
	if s == nil {
		return
	}
	_, app := s.binding()
	if app == nil {
		return
	}
	if metrics := app.metrics.Load(); metrics != nil {
		metrics.observeSubagentLifecycle(info)
		metrics.persist()
	}
}

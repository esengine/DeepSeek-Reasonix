package main

import (
	"strings"
	"time"
)

// Limits are evaluated only when the next Usage or read_file event arrives; no timer is armed.
const (
	tabTelemetryCheckpointEventLimit = 16
	tabTelemetryCheckpointMaxAge     = 30 * time.Second
)

type telemetryCheckpointState struct {
	events     int
	dirtySince time.Time
	retry      bool
	draining   bool
}

func (s *tabEventSink) turnInFlightSnapshot() bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.turnInFlight
}

func (s *tabEventSink) checkpointTelemetry(tab *WorkspaceTab, sessionPath string, force bool) {
	s.checkpointTelemetryAt(tab, sessionPath, force, time.Now())
}

func (t *WorkspaceTab) checkpointTelemetryForShutdown(sessionPath string) {
	if t == nil || t.sink == nil {
		return
	}
	t.sink.beginTelemetryDrain()
	t.sink.checkpointTelemetry(t, sessionPath, true)
}

func (s *tabEventSink) beginTelemetryDrain() {
	s.telemetryCheckpointMu.Lock()
	s.telemetryCheckpoint.draining = true
	s.telemetryCheckpointMu.Unlock()
}

// checkpointTelemetryAt synchronously saves at turn and event-count boundaries,
// or on the next Usage or read_file mutation after the age limit. Injected time keeps
// age tests deterministic; failed saves remain due for the next mutation.
func (s *tabEventSink) checkpointTelemetryAt(tab *WorkspaceTab, sessionPath string, force bool, now time.Time) {
	if s == nil || tab == nil || strings.TrimSpace(sessionPath) == "" {
		return
	}
	s.telemetryCheckpointMu.Lock()
	defer s.telemetryCheckpointMu.Unlock()

	state := &s.telemetryCheckpoint
	if !force {
		state.events++
		if state.dirtySince.IsZero() {
			state.dirtySince = now
		}
		if !state.draining && !state.retry &&
			state.events < tabTelemetryCheckpointEventLimit &&
			now.Sub(state.dirtySince) < tabTelemetryCheckpointMaxAge {
			return
		}
	}

	save := saveTelemetry
	if s.telemetrySaveHook != nil {
		save = s.telemetrySaveHook
	}
	if err := save(sessionPath+".telemetry.json", tab.telemetrySnapshot()); err != nil {
		state.retry = true
		return
	}
	draining := state.draining
	*state = telemetryCheckpointState{draining: draining}
}

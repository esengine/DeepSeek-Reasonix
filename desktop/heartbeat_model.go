package main

import (
	"log"
	"time"

	"reasonix/internal/secrets"
)

// applyConfiguredModel switches the task tab to the task's configured model
// (if any) with the controller idle, so the rebuild is synchronous instead of
// deferred behind a running turn. A failed or unready switch skips the run.
func (e *HeartbeatEngine) applyConfiguredModel(t HeartbeatTask, tabID string) (HeartbeatTask, bool) {
	if t.Model == "" {
		return t, true
	}
	apply := e.applyTaskModel
	if apply == nil {
		apply = e.app.SetModelForTab
	}
	if err := apply(tabID, t.Model); err != nil {
		log.Printf("[heartbeat] model switch for %q: %v", t.Title, secrets.RedactError(err))
		// Advance LastRunAt so a misconfigured model is retried on the next
		// scheduled tick instead of hammering the switch every 30s.
		t.LastRunAt = time.Now().UnixMilli()
		return t, false
	}
	// The rebuild replaced the controller; re-resolve and re-check it before
	// the caller submits. Transient failures skip only this tick, so LastRunAt
	// stays untouched (unlike the config error above).
	ctrl := e.app.ctrlByTabID(tabID)
	if ctrl == nil {
		log.Printf("[heartbeat] controller not ready after model switch for %q, skipping", t.Title)
		return t, false
	}
	if heartbeatControllerBusy(ctrl) {
		log.Printf("[heartbeat] controller busy after model switch for %q, skipping", t.Title)
		return t, false
	}
	return t, true
}

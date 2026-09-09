package cli

import (
	"fmt"
	"reasonix/internal/event"
	"time"
)

func (m *chatTUI) waitingRecoveryLine() (string, bool) {
	if m.recovery == nil || !m.recovery.Waiting {
		return "", false
	}
	return fmt.Sprintf("  %s waiting for provider (%s), retry in %ds — Esc to stop", m.spinner.View(), m.recovery.Phase, max(0, (m.recovery.NextAttemptAt-time.Now().UnixMilli()+999)/1000)), true
}
func (m *chatTUI) setRecoveryStatus(e event.Event) {
	m.retryAttempt = e.RetryAttempt
	m.retryMax = e.RetryMax
	m.recovery = e.Recovery
}
func (m *chatTUI) clearRecoveryStatus() { m.retryAttempt = 0; m.retryMax = 0; m.recovery = nil }

func (m *chatTUI) recordRecoveryDiscard(reason string) {
	if reason == "headers" || reason == "connect" {
		return
	}
	m.commitLine(dim("  ↻ stream interrupted — reconnecting…"))
}

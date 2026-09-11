package cli

import (
	"fmt"

	"reasonix/internal/event"
)

func (m *chatTUI) noteWatchdogRunning() {
	if m == nil || m.diagnostics == nil {
		return
	}
	ctrl := m.ctrl
	m.diagnostics.NoteRunning(func() {
		if ctrl != nil {
			ctrl.Cancel()
		}
	})
}

func (m *chatTUI) noteWatchdogIdle() {
	if m == nil || m.diagnostics == nil {
		return
	}
	m.diagnostics.NoteIdle()
}

func (m *chatTUI) noteWatchdogHeartbeat(source string) {
	if m == nil || m.diagnostics == nil {
		return
	}
	m.diagnostics.NoteActiveHeartbeat(source)
}

func watchdogAgentSource(kind event.Kind) string {
	return fmt.Sprintf("agent:%d", kind)
}

func (m *chatTUI) updateWatchdogStatusProvider() {
	if m == nil || m.diagnostics == nil || m.ctrl == nil {
		return
	}
	ctrl := m.ctrl
	m.diagnostics.SetStatusProvider(func() string {
		st := ctrl.RuntimeStatus()
		return fmt.Sprintf("running=%v pending_prompt=%v cancel_requested=%v cancellable=%v background_jobs=%d",
			st.Running, st.PendingPrompt, st.CancelRequested, st.Cancellable, st.BackgroundJobs)
	})
}

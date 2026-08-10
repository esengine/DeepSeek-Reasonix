package cli

import (
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// enableMouseTracking is the cell-motion + SGR enable sequence matching what
// View() requests via tea.MouseModeCellMotion. Windows Terminal / ConPTY can
// drop mouse tracking after long turns, resize storms, or focus loss (#7583);
// re-sending this sequence restores wheel → MouseWheelMsg instead of bare
// Up/Down history cycling.
const enableMouseTracking = ansi.SetModeMouseButtonEvent + ansi.SetModeMouseExtSgr

// mouseReenableMinInterval bounds how often we re-emit enableMouseTracking.
// Requests inside the window set a pending flag and arm a trailing-edge timer
// so the *last* resize/focus in a storm still re-enables after the quiet period.
const mouseReenableMinInterval = 500 * time.Millisecond

// mouseReenableMsg is delivered after the rate-limit quiet period so a pending
// trailing re-enable can fire without sleeping in tests (inject the msg).
// Only one timer is armed at a time (mouseReenableTimerArmed); later requests
// just set mouseReenablePending, so no generation/seq is required.
type mouseReenableMsg struct{}

// maybeReenableMouse returns a tea.Raw cmd that re-enables mouse tracking when
// capture is on and the rate limit allows. Inside the rate-limit window it
// records a pending trailing request and arms a one-shot timer (if needed)
// instead of dropping the request permanently.
//
// No-op for Termux native scrollback (mouse is intentionally off so the soft
// keyboard can focus) and when the user has toggled capture off via /mouse.
func (m *chatTUI) maybeReenableMouse() tea.Cmd {
	if m.nativeScrollback || m.mouseCaptureOff {
		m.mouseReenablePending = false
		return nil
	}
	now := m.mouseNow()
	if !m.lastMouseReenable.IsZero() && now.Sub(m.lastMouseReenable) < mouseReenableMinInterval {
		m.mouseReenablePending = true
		return m.armMouseReenableTimer(now)
	}
	return m.emitMouseReenable(now)
}

// armMouseReenableTimer schedules a trailing-edge fire for the remaining
// rate-limit window. Only one timer is armed at a time; later requests just
// set mouseReenablePending.
func (m *chatTUI) armMouseReenableTimer(now time.Time) tea.Cmd {
	if m.mouseReenableTimerArmed {
		return nil
	}
	wait := max(mouseReenableMinInterval-now.Sub(m.lastMouseReenable), 0)
	m.mouseReenableTimerArmed = true
	return tea.Tick(wait, func(time.Time) tea.Msg {
		return mouseReenableMsg{}
	})
}

// emitMouseReenable sends the enable sequence and clears trailing state.
func (m *chatTUI) emitMouseReenable(now time.Time) tea.Cmd {
	m.lastMouseReenable = now
	m.mouseReenablePending = false
	m.mouseReenableTimerArmed = false
	return tea.Raw(enableMouseTracking)
}

// handleMouseReenableMsg processes a trailing-edge timer. If a request was
// still pending after the quiet period, emit enable now.
func (m *chatTUI) handleMouseReenableMsg(_ mouseReenableMsg) tea.Cmd {
	m.mouseReenableTimerArmed = false
	if !m.mouseReenablePending {
		return nil
	}
	// Pending request survived the quiet period — fire now (and allow another
	// trailing cycle if more requests arrive inside the next window).
	return m.maybeReenableMouse()
}

// mouseNow is the clock for rate limiting. Tests replace mouseNowFn.
func (m *chatTUI) mouseNow() time.Time {
	if mouseNowFn != nil {
		return mouseNowFn()
	}
	return time.Now()
}

// mouseNowFn, when non-nil, overrides time.Now for mouse re-enable tests.
var mouseNowFn func() time.Time

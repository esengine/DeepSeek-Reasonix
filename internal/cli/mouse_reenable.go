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
// Up/Down history cycling. Rate-limited so resize spam does not flood the TTY.
//
// Defined in this file so chat_tui.go can concatenate resetThenEnableMouseTracking.
const enableMouseTracking = ansi.SetModeMouseButtonEvent + ansi.SetModeMouseExtSgr

// mouseReenableMinInterval bounds how often we re-emit enableMouseTracking.
const mouseReenableMinInterval = 500 * time.Millisecond

// maybeReenableMouse returns a tea.Raw cmd that re-enables mouse tracking when
// capture is on and the rate limit allows. No-op for Termux native scrollback
// (mouse is intentionally off so the soft keyboard can focus) and when the
// user has toggled capture off via /mouse.
func (m *chatTUI) maybeReenableMouse() tea.Cmd {
	if m.nativeScrollback || m.mouseCaptureOff {
		return nil
	}
	now := time.Now()
	if !m.lastMouseReenable.IsZero() && now.Sub(m.lastMouseReenable) < mouseReenableMinInterval {
		return nil
	}
	m.lastMouseReenable = now
	return tea.Raw(enableMouseTracking)
}

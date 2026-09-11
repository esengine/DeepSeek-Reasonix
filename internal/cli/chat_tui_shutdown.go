// chat_tui_shutdown.go — the one exit every quit gesture and signal funnels into.
package cli

import (
	"fmt"
	"os"
	"sync/atomic"

	tea "charm.land/bubbletea/v2"

	"reasonix/internal/i18n"
)

const (
	tuiShutdownPending uint32 = iota
	tuiShutdownCompleted
	tuiShutdownFallbackClaimed
)

// tuiShutdownCompletion arbitrates the boundary between a completed graceful
// exit and the watchdog fallback. Exactly one side wins, so a timer callback
// cannot turn a snapshot that already completed into ErrProgramKilled.
type tuiShutdownCompletion struct {
	state atomic.Uint32
	done  chan struct{}
}

func newTUIShutdownCompletion() *tuiShutdownCompletion {
	return &tuiShutdownCompletion{done: make(chan struct{})}
}

func (c *tuiShutdownCompletion) complete() {
	if c != nil && c.state.CompareAndSwap(tuiShutdownPending, tuiShutdownCompleted) {
		if c.done != nil {
			close(c.done)
		}
	}
}

func (c *tuiShutdownCompletion) claimFallback() bool {
	return c != nil && c.state.CompareAndSwap(tuiShutdownPending, tuiShutdownFallbackClaimed)
}

// shutdownAndQuit persists what the controller holds beyond the last snapshot
// and leaves. The shutdown variant writes a recovery branch when another
// process keeps the active session's compatibility lock for the bounded wait.
func (m chatTUI) shutdownAndQuit(completion *tuiShutdownCompletion) (tea.Model, tea.Cmd) {
	defer completion.complete()
	// Only snapshot if we still own the session (no takeover, or takeover returned).
	if m.ctrl != nil && (m.takeover == nil || !m.takeover.Returned()) {
		m.shutdownErr = m.ctrl.SnapshotForShutdown()
		m.followSessionLease()
	}
	return m, tea.Quit
}

// reportShutdownFailure runs after Bubble Tea restores the terminal, when a
// final-save failure can be printed without corrupting the alt screen.
func reportShutdownFailure(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
	}
}

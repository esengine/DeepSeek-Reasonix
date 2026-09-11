package cli

// toggleInboxPaused handles the queue-only space shortcut outside the main
// Update state machine so inbox UX does not add another branch to that already
// large input dispatcher.
func (m *chatTUI) toggleInboxPaused() {
	snap := m.inboxSnap()
	paused := !snap.Paused
	_ = m.ctrl.SetInboxPaused(paused)
	if paused {
		m.notice("inbox paused")
		return
	}
	m.notice("inbox resumed")
}

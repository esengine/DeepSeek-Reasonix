package control

func (d *dispatchState) sessionState() *dispatchSession {
	if state := d.session.Load(); state != nil {
		return state
	}
	state := &dispatchSession{}
	if d.session.CompareAndSwap(nil, state) {
		return state
	}
	return d.session.Load()
}

// InheritDispatchSession retains pairings only for the same canonical session
// file. Sharing the state keeps signing counters and replay protection monotonic
// while both controller generations still exist during a replacement.
func (c *Controller) InheritDispatchSession(previous *Controller, path string) {
	if previous == nil || sessionPeerKey(path) == "" || sessionPeerKey(path) != sessionPeerKey(previous.SessionPath()) {
		return
	}
	c.dispatch.session.Store(previous.dispatch.sessionState())
}

package control

// SetIdleObserver installs the owner's notification after all turn/rotation work settles.
func (c *Controller) SetIdleObserver(observer func()) {
	c.mu.Lock()
	c.idleObserver = observer
	c.mu.Unlock()
}

func (c *Controller) notifyIdle() {
	c.mu.Lock()
	observer := c.idleObserver
	if c.gate.busy() {
		observer = nil
	}
	c.mu.Unlock()
	if observer != nil {
		observer()
	}
}

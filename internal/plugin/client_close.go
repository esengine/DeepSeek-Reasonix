package plugin

func (c *Client) close() {
	if c == nil {
		return
	}
	c.closeOnce.Do(func() {
		c.closed.Store(true)
		c.refresh.mu.Lock()
		c.refresh.closed = true
		c.refresh.onChanged = nil
		stopNotifications := c.refresh.stopNotifications
		c.refresh.stopNotifications = nil
		cancelRefresh := c.refresh.cancel
		c.refresh.mu.Unlock()

		if stopNotifications != nil {
			stopNotifications()
		}
		if cancelRefresh != nil {
			cancelRefresh()
		}
		c.surfaceStopsMu.Lock()
		surfaceStops := append([]func(){}, c.surfaceStops...)
		c.surfaceStops = nil
		c.surfaceStopsMu.Unlock()
		for _, stop := range surfaceStops {
			stop()
		}
		if c.t != nil {
			c.t.close()
		}
	})
}

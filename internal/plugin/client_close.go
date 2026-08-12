package plugin

func (c *Client) close() {
	if c == nil {
		return
	}
	c.closeOnce.Do(func() {
		c.refresh.mu.Lock()
		c.refresh.closed = true
		c.refresh.pending = false
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
		if c.t != nil {
			c.t.close()
		}
	})
}

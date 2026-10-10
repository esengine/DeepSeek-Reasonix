package plugin

func (c *Client) close() {
	if c == nil || c.t == nil {
		return
	}
	c.stopToolListWatch()
	c.t.close()
}

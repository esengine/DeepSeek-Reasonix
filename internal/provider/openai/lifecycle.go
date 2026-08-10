package openai

// CloseIdleConnections releases pooled HTTP connections for bounded probes.
func (c *client) CloseIdleConnections() {
	if c != nil && c.http != nil {
		c.http.CloseIdleConnections()
	}
}

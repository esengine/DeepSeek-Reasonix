package control

import "reasonix/internal/contract/provider"

// History returns the executor's current message log (for repopulating a
// resumed frontend's view).
func (c *Controller) History() []provider.Message {
	if c.executor == nil {
		return nil
	}
	return c.executor.Session().Snapshot() // copy — a turn may be appending concurrently
}

// HistoryLen returns the number of messages in the live log.
func (c *Controller) HistoryLen() int {
	if c.executor == nil {
		return 0
	}
	return c.executor.Session().Len()
}

// HistoryWindow returns a copy of the messages in [start, end) of the live
// log. Paging frontends use it to convert a display window without copying
// the whole history.
func (c *Controller) HistoryWindow(start, end int) []provider.Message {
	if c.executor == nil {
		return []provider.Message{}
	}
	return c.executor.Session().MessageRange(start, end)
}

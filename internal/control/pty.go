package control

import (
	"reasonix/internal/pty"
)

// PTY returns the persistent PTY terminal manager.
// Hot rebuilds pass this to the replacement Controller so running interactive
// sessions survive model/settings swaps. Nil only when constructed without one.
func (c *Controller) PTY() *pty.Manager {
	if c == nil {
		return nil
	}
	return c.pty
}

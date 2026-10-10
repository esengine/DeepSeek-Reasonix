package control

import (
	"fmt"
	"strings"

	"reasonix/internal/ext/plugin"
)

// ErrMCPNothingHeld is a trust request for a server with no held definitions.
var ErrMCPNothingHeld = plugin.ErrNoHeldTools

// ErrMCPDigestMismatch is a trust request bound to definitions the server no
// longer holds.
var ErrMCPDigestMismatch = plugin.ErrHeldDigestMismatch

// AcceptMCPHeldTools approves the definitions the server's live connection
// withheld, if digest names exactly that set, and reconnects it so those tools register. It returns the tool count
// of the reconnected server.
func (c *Controller) AcceptMCPHeldTools(name, digest string) (int, error) {
	name = strings.TrimSpace(name)
	host := c.mcp.hostRef()
	if host == nil {
		return 0, ErrMCPNothingHeld
	}
	if _, err := host.AcceptHeldTools(name, strings.TrimSpace(digest)); err != nil {
		return 0, fmt.Errorf("accept held tools of %q: %w", name, err)
	}
	return c.ReconnectMCPServer(name)
}

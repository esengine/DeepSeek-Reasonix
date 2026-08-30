package control

import "reasonix/internal/sandbox"

// BoundShell reports the interpreter this controller generation resolved at
// build time — the same shell the bash tool and "!" commands run under. Hosts
// compare it against a fresh ResolveShell to tell "current session" from
// "after reload".
func (c *Controller) BoundShell() sandbox.Shell {
	return c.shell
}

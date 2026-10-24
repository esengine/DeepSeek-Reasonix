package control

import (
	"log/slog"
	"sort"

	"reasonix/internal/safety/permission"
	"reasonix/internal/state/sessionstore"
)

// persistSessionGrants writes what the person allowed for this conversation
// into its sidecar, so resuming it does not ask the same question again.
func (c *Controller) persistSessionGrants() {
	path := c.SessionPath()
	if path == "" {
		return
	}
	c.grantsMu.Lock()
	defer c.grantsMu.Unlock()
	grants := c.approval.sessionGrants()
	sort.Strings(grants)
	if err := sessionstore.SaveSessionGrants(path, grants); err != nil {
		slog.Warn("controller: remember session grants", "err", err)
	}
}

// restoreSessionGrants brings back the answers a resumed conversation was given.
func (c *Controller) restoreSessionGrants(path string) {
	var grants []string
	for _, rule := range sessionstore.LoadSessionGrants(path) {
		if permission.ValidSessionGrantRule(rule) {
			grants = append(grants, rule)
		}
	}
	if len(grants) == 0 {
		return
	}
	c.approval.restoreSessionAuthorizations(SessionAuthorizations{Grants: grants})
}

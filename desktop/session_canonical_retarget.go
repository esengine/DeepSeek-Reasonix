package main

import (
	"context"
	"strings"

	"reasonix/internal/sessioncatalog"
)

// resolveCanonicalSessionPath returns a unique adopted/canonical leaf for the
// topic that owns path, when the catalog has one. Empty means keep path.
// Retarget happens before Controller create/rebind so the new controller leases
// and binds authority on the canonical path only.
func (a *App) resolveCanonicalSessionPath(path string) string {
	if a == nil || strings.TrimSpace(path) == "" {
		return ""
	}
	catalog := a.sessionCatalog.Load()
	if catalog == nil {
		return ""
	}
	ctx := context.Background()
	rec, ok, err := catalog.GetSession(ctx, path)
	if err != nil || !ok {
		return ""
	}
	if rec.TopicID == "" {
		// Group by recovery group when topic is unset.
		if rec.RecoveryCanonical && (rec.RecoveryRole == sessioncatalog.RecoveryRoleAdopted || rec.RecoveryRole == sessioncatalog.RecoveryRolePreferred) {
			return ""
		}
		return ""
	}
	topic, ok, err := catalog.GetTopic(ctx, sessioncatalog.TopicKey{Scope: rec.Scope, WorkspaceRoot: rec.WorkspaceRoot, TopicID: rec.TopicID})
	if err != nil || !ok {
		return ""
	}
	return sessioncatalog.CanonicalSessionPathForTopic(topic.Sessions, path)
}

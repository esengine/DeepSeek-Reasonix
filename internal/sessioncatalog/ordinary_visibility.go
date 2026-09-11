package sessioncatalog

import (
	"context"
	"path/filepath"
	"strings"

	"reasonix/internal/agent"
)

// PreferredOrdinarySessionPaths returns the session paths that may appear in
// the ordinary project tree for one workspace. Covered recovery copies and
// non-preferred conflict forks are omitted so the sidebar matches the 1.23
// contract: one conversation row, not a wall of recovery replicas.
//
// Rules:
//   - Every non-recovered session is preferred.
//   - When a recovery group still has its non-recovered parent, no recovered
//     member of that group is preferred (parent is the ordinary row).
//   - Otherwise the group keeps a single preferred recovered leaf: canonical
//     or adopted first, then highest turns, then latest activity.
//
// Open/running tabs may still force-show a path outside this set at the UI
// boundary; this helper only encodes on-disk ordinary visibility.
func (c *Catalog) PreferredOrdinarySessionPaths(ctx context.Context, scope, workspaceRoot string) (map[string]struct{}, error) {
	out := map[string]struct{}{}
	if c == nil || c.db == nil {
		return out, nil
	}
	scope, workspaceRoot = normalizeScope(scope, workspaceRoot)
	rows, err := c.db.QueryContext(ctx, `
		SELECT path, recovered, parent_id, recovery_copy, recovery_group_id,
		       recovery_role, recovery_canonical, turns, last_activity_at
		FROM catalog_sessions
		WHERE scope=? AND workspace_root_key=? AND missing_since=0 AND health<>'missing'`,
		scope, c.workspaceRootKey(scope, workspaceRoot))
	if err != nil {
		return out, err
	}
	defer rows.Close()
	var sessions []SessionRecord
	for rows.Next() {
		var rec SessionRecord
		var recoveryCopy, recoveryCanonical, recovered int
		if err := rows.Scan(&rec.Path, &recovered, &rec.ParentID, &recoveryCopy,
			&rec.RecoveryGroupID, &rec.RecoveryRole, &recoveryCanonical,
			&rec.Turns, &rec.LastActivityAt); err != nil {
			return map[string]struct{}{}, err
		}
		rec.Recovered = recovered != 0
		rec.RecoveryCopy = recoveryCopy != 0
		rec.RecoveryCanonical = recoveryCanonical != 0
		if rec.RecoveryRole == "" {
			if rec.RecoveryCopy {
				rec.RecoveryRole = RecoveryRoleCoveredCopy
			} else if rec.Recovered {
				rec.RecoveryRole = RecoveryRoleDiverged
			} else {
				rec.RecoveryRole = RecoveryRoleNormal
			}
		}
		sessions = append(sessions, rec)
	}
	if err := rows.Err(); err != nil {
		return map[string]struct{}{}, err
	}
	return PreferredOrdinarySessionPaths(sessions), nil
}

// PreferredOrdinarySessionPaths selects ordinary-tree paths from an in-memory
// session set. Pure helper for tests and callers that already hold records.
func PreferredOrdinarySessionPaths(sessions []SessionRecord) map[string]struct{} {
	preferred := make(map[string]struct{}, len(sessions))
	normalRoots := map[string]struct{}{}
	recoveredByGroup := map[string][]SessionRecord{}
	for _, session := range sessions {
		path := strings.TrimSpace(session.Path)
		if path == "" {
			continue
		}
		if session.RecoveryCopy || session.RecoveryRole == RecoveryRoleCoveredCopy {
			continue
		}
		if !session.Recovered {
			preferred[path] = struct{}{}
			normalRoots[agent.BranchID(path)] = struct{}{}
			continue
		}
		key := recoveryLineageKey(session)
		recoveredByGroup[key] = append(recoveredByGroup[key], session)
	}
	for group, members := range recoveredByGroup {
		if _, parentPresent := normalRoots[group]; parentPresent {
			// Parent conversation still exists: keep only that row.
			continue
		}
		best := pickPreferredRecovery(members)
		if path := strings.TrimSpace(best.Path); path != "" {
			preferred[path] = struct{}{}
		}
	}
	return preferred
}

// OrdinaryTreeSession reports whether a catalog session should appear in the
// ordinary project tree given open/running runtime state and the preferred set.
func OrdinaryTreeSession(session SessionRecord, open, running bool, preferred map[string]struct{}) bool {
	if open || running {
		return true
	}
	if session.RecoveryCopy || session.RecoveryRole == RecoveryRoleCoveredCopy {
		return false
	}
	if !session.Recovered {
		return true
	}
	if preferred == nil {
		// Without a preference set, still hide covered copies (handled above)
		// but keep recovered leaves so callers that skip preference computation
		// do not blank the tree. Prefer building PreferredOrdinarySessionPaths.
		return true
	}
	_, ok := preferred[strings.TrimSpace(session.Path)]
	return ok
}

func recoveryLineageKey(session SessionRecord) string {
	if group := strings.TrimSpace(session.RecoveryGroupID); group != "" {
		return group
	}
	if parent := strings.TrimSpace(session.ParentID); parent != "" {
		return parent
	}
	// Isolated recovered leaf without parent metadata: its own group.
	return "path:" + agent.BranchID(session.Path)
}

func pickPreferredRecovery(members []SessionRecord) SessionRecord {
	if len(members) == 0 {
		return SessionRecord{}
	}
	best := members[0]
	for _, candidate := range members[1:] {
		if recoveryRank(candidate) > recoveryRank(best) {
			best = candidate
			continue
		}
		if recoveryRank(candidate) < recoveryRank(best) {
			continue
		}
		if candidate.Turns != best.Turns {
			if candidate.Turns > best.Turns {
				best = candidate
			}
			continue
		}
		if candidate.LastActivityAt != best.LastActivityAt {
			if candidate.LastActivityAt > best.LastActivityAt {
				best = candidate
			}
			continue
		}
		if filepath.Base(candidate.Path) < filepath.Base(best.Path) {
			best = candidate
		}
	}
	return best
}

func recoveryRank(session SessionRecord) int {
	switch {
	case session.RecoveryCanonical:
		return 3
	case session.RecoveryRole == RecoveryRoleAdopted:
		return 2
	case session.RecoveryRole == RecoveryRolePreferred:
		return 2
	case session.RecoveryRole == RecoveryRoleDiverged || session.Recovered:
		return 1
	default:
		return 0
	}
}

package sessioncatalog

import (
	"context"
	"path/filepath"
)

// RecoveryGroup is a catalog projection of one proved recovery lineage. It is
// diagnostic input only; destructive callers must revalidate file contents and
// locks through agent helpers before moving anything.
type RecoveryGroup struct {
	ID            string          `json:"id"`
	Directory     string          `json:"directory"`
	RootPath      string          `json:"rootPath,omitempty"`
	CanonicalPath string          `json:"canonicalPath,omitempty"`
	State         string          `json:"state"`
	Members       []SessionRecord `json:"members"`
}

// ListRecoveryGroups returns every indexed recovery lineage in directory. All
// slices are initialized for stable CLI/Wails JSON contracts.
func (c *Catalog) ListRecoveryGroups(ctx context.Context, directory string) ([]RecoveryGroup, error) {
	out := []RecoveryGroup{}
	if c == nil || c.db == nil {
		return out, nil
	}
	directory = cleanCatalogAccessPath(directory)
	rows, err := c.db.QueryContext(ctx, `SELECT `+sessionSelectColumns+` FROM catalog_sessions
		WHERE directory_key=? AND recovered=1 AND recovery_group_id<>'' AND missing_since=0
		ORDER BY recovery_group_id,path`, c.pathKey(directory))
	if err != nil {
		return out, err
	}
	defer rows.Close()
	byID := map[string]int{}
	for rows.Next() {
		record, err := scanSession(rows)
		if err != nil {
			return []RecoveryGroup{}, err
		}
		index, ok := byID[record.RecoveryGroupID]
		if !ok {
			index = len(out)
			byID[record.RecoveryGroupID] = index
			out = append(out, RecoveryGroup{ID: record.RecoveryGroupID, Directory: directory,
				RootPath: filepath.Join(directory, record.RecoveryGroupID+".jsonl"), Members: []SessionRecord{}})
		}
		group := &out[index]
		group.Members = append(group.Members, record)
		if record.RecoveryCanonical && record.RecoveryRole == RecoveryRolePreferred {
			group.CanonicalPath = record.Path
			group.State = "preferred"
		} else if record.RecoveryCanonical && record.RecoveryRole == RecoveryRoleAdopted {
			group.CanonicalPath = record.Path
			group.State = "adopted"
		} else if group.State == "" && record.RecoveryRole == RecoveryRoleDiverged {
			group.State = "diverged"
		}
	}
	if err := rows.Err(); err != nil {
		return []RecoveryGroup{}, err
	}
	for index := range out {
		if out[index].State == "" {
			out[index].State = "repairing"
		}
	}
	return out, nil
}

package workspacestate

import (
	"bytes"
	"encoding/json"
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"reasonix/internal/pathidentity"
)

// WorkspaceIndex resolves every persisted workspace root once and reuses the
// result for the lifetime of one read projection. Sidebar snapshots commonly
// resolve the same roots several times; doing that through ResolveWorkspaceID
// would walk every registered root for every sidebar row.
type WorkspaceIndex struct {
	exact    map[string]string
	physical map[string]string
	resolved map[string]string
}

func NewWorkspaceIndex(state State) *WorkspaceIndex {
	index := &WorkspaceIndex{
		exact: map[string]string{}, physical: map[string]string{}, resolved: map[string]string{},
	}
	groups := map[string][]string{}
	keys := map[string]string{}
	for id, workspace := range state.Workspaces {
		root := strings.TrimSpace(workspace.Root)
		if root == "" {
			continue
		}
		identity, err := pathidentity.Resolve(root, pathidentity.Options{FollowLeaf: true})
		if err != nil || identity.Key == "" {
			continue
		}
		groups[identity.Key] = append(groups[identity.Key], id)
		keys[id] = identity.Key
	}
	for key, ids := range groups {
		slices.Sort(ids)
		index.physical[key] = canonicalWorkspaceOwner(state, ids)
	}
	for id, workspace := range state.Workspaces {
		if owner := index.physical[keys[id]]; owner != "" {
			index.exact[filepath.Clean(strings.TrimSpace(workspace.Root))] = owner
		}
	}
	return index
}

// Resolve returns the persisted owner for root without rescanning registered
// roots. Exact persisted spellings are filesystem-free; aliases are resolved
// once and cached for the rest of the snapshot.
func (i *WorkspaceIndex) Resolve(root string) (string, bool, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return "", false, nil
	}
	clean := filepath.Clean(root)
	if id := i.exact[clean]; id != "" {
		return id, true, nil
	}
	if id, ok := i.resolved[clean]; ok {
		return id, id != "", nil
	}
	identity, err := pathidentity.Resolve(root, pathidentity.Options{FollowLeaf: true})
	if err != nil {
		return "", false, err
	}
	id := i.physical[identity.Key]
	i.resolved[clean] = id
	return id, id != "", nil
}

// FindWorkspace admits the read-only path only for an unambiguous physical
// owner. Missing or duplicate roots go through the registration transaction.
func FindWorkspace(state State, root string) (string, bool) {
	if strings.TrimSpace(root) == "" {
		return "", false
	}
	identity, err := pathidentity.Resolve(root, pathidentity.Options{FollowLeaf: true})
	if err != nil {
		return "", false
	}
	matches := matchingWorkspaceIDs(state, identity)
	if len(matches) != 1 {
		return "", false
	}
	return matches[0], true
}

// Clone preserves unknown fields while isolating every mutable organization
// field. Read-side import planning must never edit a published snapshot.
func (o Organization) Clone() Organization {
	o.Order = slices.Clone(o.Order)
	o.Imported = maps.Clone(o.Imported)
	o.extra = cloneUnknownFields(o.extra)
	o.Groups = slices.Clone(o.Groups)
	for i := range o.Groups {
		o.Groups[i].Members = slices.Clone(o.Groups[i].Members)
		o.Groups[i].extra = cloneUnknownFields(o.Groups[i].extra)
	}
	return o
}

func cloneUnknownFields(fields map[string]json.RawMessage) map[string]json.RawMessage {
	if fields == nil {
		return nil
	}
	clone := make(map[string]json.RawMessage, len(fields))
	for key, value := range fields {
		clone[key] = bytes.Clone(value)
	}
	return clone
}

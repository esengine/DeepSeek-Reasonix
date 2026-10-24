package main

import (
	"os"
	"strings"
	"sync"

	"reasonix/desktop/internal/workspacestate"
	"reasonix/internal/agent"
	"reasonix/internal/store"
)

type sourceHeadObservation struct {
	info  os.FileInfo
	index *agent.SessionHeadIndex
}

var sourceHeadRows sync.Map

// A mapped DAG head must not return as an unidentified path placeholder while
// its index is unavailable. This is a display filter, not an ownership alias:
// independently identified siblings remain eligible for their own rows.
// Adoption belongs to the source, not to the workspace a sidecar lists it in.
func adoptedSourceRows(state workspacestate.State) map[string]bool {
	adopted := map[string]bool{}
	for _, mapping := range state.SourceMappings {
		for _, key := range state.SourceKeys(mapping.SourceKey) {
			adopted["source\x00local\x00"+key] = true
		}
		adopted["source\x00local\x00"+desktopSourceKey(mapping.Path, "")] = true
		if sourceMappingHasPathAlias(mapping) {
			adopted[sessionRuntimeKey(mapping.Path)] = true
		}
	}
	return adopted
}

// A single-head DAG is displayed by path, while upgrades record its head ID.
// Use the same path alias in every projection so retained originals cannot
// reappear after their canonical session is archived. Multi-head rows keep
// independent identities; adopting one must never hide its siblings.
func sourceMappingHasPathAlias(mapping workspacestate.SourceMapping) bool {
	// A reviewed version is a separate conversation, not another owner of
	// the original path. Only its versioned SourceKey may alias that session.
	if strings.Contains(mapping.SourceKey, ":review:") {
		return false
	}
	if mapping.HeadID == "" {
		return true
	}
	heads, err := sessionSourceHeads(mapping.Path)
	if err != nil {
		return false
	}
	visible := 0
	selected := false
	for _, head := range heads {
		if head.Retired {
			continue
		}
		if head.Kind != agent.HeadKindConcurrent {
			visible++
		}
		if head.Selected && head.ID == mapping.HeadID {
			selected = true
		}
	}
	return selected && visible <= 1
}

// Listing consumes only the published head index. Replaying an event log here
// would make sidebar pagination perform content work and contend with writers.
// Missing/stale indices degrade to one path row and are repaired separately.
func sessionSourceHeads(path string) ([]agent.SessionHead, error) {
	if _, err := os.Stat(path); err != nil {
		sourceHeadRows.Delete(path)
		return nil, err
	}
	info, err := os.Stat(store.SessionEventIndex(path))
	if err != nil {
		sourceHeadRows.Delete(path)
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	// The checkpoint, event log, and head index are published independently.
	// Cache only the decoded index, keyed by that file's own identity, and
	// validate its log coverage on every read. A transient missing/stale index
	// must not survive publication merely because the checkpoint is unchanged.
	var index *agent.SessionHeadIndex
	if cached, ok := sourceHeadRows.Load(path); ok {
		entry := cached.(sourceHeadObservation)
		if os.SameFile(entry.info, info) && entry.info.Size() == info.Size() && entry.info.ModTime().Equal(info.ModTime()) {
			index = entry.index
		}
	}
	if index == nil {
		index, err = agent.ReadSessionHeadIndex(path)
		if err != nil || index == nil {
			sourceHeadRows.Delete(path)
			return nil, err
		}
		sourceHeadRows.Store(path, sourceHeadObservation{info: info, index: index})
	}
	if !index.Current(path) {
		return nil, nil
	}
	return index.Heads, nil
}

func expandSessionSourceRows(node ProjectNode) []ProjectNode {
	if node.Session != nil || node.SessionPath == "" || node.RecoveryState == "recovery_only" {
		return []ProjectNode{node}
	}
	heads, err := sessionSourceHeads(node.SessionPath)
	if err != nil {
		node.Health = "degraded"
	}
	live := []agent.SessionHead{}
	for _, head := range heads {
		if !head.Retired && head.Kind != agent.HeadKindConcurrent {
			live = append(live, head)
		}
	}
	if len(live) <= 1 {
		headID := ""
		if len(live) == 1 {
			headID = live[0].ID
		}
		node.Source = &SessionSourceRef{HostID: localDesktopHostID, Path: node.SessionPath, HeadID: headID, SourceKey: desktopSourceKey(node.SessionPath, headID)}
		node.Historical = true
		return []ProjectNode{node}
	}
	rows := []ProjectNode{}
	for _, head := range live {
		row := node
		row.Source = &SessionSourceRef{HostID: localDesktopHostID, Path: node.SessionPath, HeadID: head.ID, SourceKey: desktopSourceKey(node.SessionPath, head.ID)}
		row.Historical, row.HistoricalBranch = true, true
		row.Key = "source_" + row.Source.SourceKey
		row.Turns, row.Preview = head.Turns, head.Preview
		if !head.LastActivity.IsZero() {
			row.LastActivityAt = head.LastActivity.UnixMilli()
		}
		if !head.CreatedAt.IsZero() {
			row.CreatedAt = head.CreatedAt.UnixMilli()
		}
		rows = append(rows, row)
	}
	return rows
}

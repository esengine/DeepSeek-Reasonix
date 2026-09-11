package main

import (
	"errors"
	"path/filepath"
	"strings"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/control"
	"reasonix/internal/sessioncatalog"
)

// sessionHeadLineageState is the lineage view state for a schema-2 session:
// its versions are heads of one log, never separate files.
const sessionHeadLineageState = "heads"

// sessionHeadQuietPeriod is how long a covered head must have been idle before
// cleanup retires it; the writer that forked it seconds ago may still be on it.
var sessionHeadQuietPeriod = time.Minute

// tabMetaAfterHeadSwitch publishes a tab whose controller replaced the
// transcript under the same session path. The generation bump makes the
// frontend rehydrate instead of patching the surface it showed before.
func (a *App) tabMetaAfterHeadSwitch(tab *WorkspaceTab) TabMeta {
	if tab == nil {
		return TabMeta{}
	}
	a.mu.Lock()
	tab.SessionGeneration++
	if tab.sink != nil {
		tab.sink.setSessionGeneration(tab.SessionGeneration)
	}
	meta := a.tabMeta(tab, a.activeTabID == tab.ID)
	a.mu.Unlock()
	a.invalidatePromptHistoryCache()
	if meta.SessionPath != "" {
		a.emitProjectTreeChangedForSessionDirs(sessionDirectoryForPath(meta.SessionPath))
	}
	return meta
}

func (a *App) tabForSessionPath(path string) (*WorkspaceTab, control.SessionAPI) {
	key := sessionRuntimeKey(path)
	if key == "" {
		return nil, nil
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	for _, tab := range a.runtimeTabsLocked() {
		if sessionRuntimeKey(tab.currentSessionPath()) == key {
			return tab, tab.Ctrl
		}
	}
	return nil, nil
}

func topicRecordForLineage(topic sessioncatalog.TopicRecord, selectedPath string) (sessioncatalog.SessionRecord, bool) {
	want := selectedPath
	if sessioncatalog.PathIdentityKey(want) == "" {
		want = topic.RepresentativePath
	}
	for _, record := range topic.Sessions {
		if sameRecoveryLineagePath(record.Path, want) {
			return record, true
		}
	}
	return sessioncatalog.SessionRecord{}, false
}

// sessionHeadLineage answers the versions view from the log itself when the
// selected session is schema 2; a schema-1 session reports no heads.
func (a *App) sessionHeadLineage(topic sessioncatalog.TopicRecord, selectedPath string) (RecoveryLineageView, bool) {
	record, ok := topicRecordForLineage(topic, selectedPath)
	if !ok {
		return RecoveryLineageView{}, false
	}
	heads, err := agent.ListSessionHeads(record.Path)
	if err != nil || len(heads) == 0 {
		return RecoveryLineageView{}, false
	}
	return a.headLineageView(record, heads), true
}

func (a *App) headLineageView(record sessioncatalog.SessionRecord, heads []agent.SessionHead) RecoveryLineageView {
	out := RecoveryLineageView{GroupID: agent.BranchID(record.Path), State: sessionHeadLineageState, Members: []RecoveryLineageMember{}}
	_, overlays := a.catalogRuntimeOverlays()
	overlay := overlays[sessionRuntimeKey(record.Path)]
	title := record.CustomTitle
	if meta, ok, err := agent.LoadBranchMeta(record.Path); err == nil && ok {
		title = meta.CustomTitle
	}
	for _, head := range heads {
		if head.Retired {
			continue
		}
		note := head.Name
		if head.ID == agent.SessionMainHead && note == "" {
			note = title
		}
		out.Members = append(out.Members, RecoveryLineageMember{
			Path: record.Path, HeadID: head.ID, HeadKind: head.Kind, HeadName: head.Name, Selected: head.Selected,
			VersionKind: "head", VersionState: string(agent.VersionActive), ParentVersionID: head.ParentHead,
			Role: sessioncatalog.RecoveryRoleNormal, Canonical: head.Selected, Turns: head.Turns,
			Open: head.Selected && overlay.open, Running: head.Selected && overlay.running,
			VersionNote: note, Preview: head.Preview,
			CreatedAt: unixMilliOrZero(head.CreatedAt), LastActivityAt: unixMilliOrZero(head.LastActivity),
		})
		out.BranchCount++
		if head.Covered {
			out.CleanupEligible++
		}
	}
	return out
}

// mergeHeadLineage folds the heads of a log into the file lineage of the same
// conversation. The log's own file row gives way to its heads; the selected
// head is canonical only when that row was, or when no file lineage exists.
func mergeHeadLineage(file, heads RecoveryLineageView) RecoveryLineageView {
	if len(heads.Members) == 0 {
		return file
	}
	logPath := heads.Members[0].Path
	out := heads
	selectedIsCanonical := len(file.Members) == 0
	for _, member := range file.Members {
		if sameRecoveryLineagePath(member.Path, logPath) {
			selectedIsCanonical = member.Canonical
			continue
		}
		out.Members = append(out.Members, member)
	}
	if !selectedIsCanonical {
		for i := range out.Members[:len(heads.Members)] {
			out.Members[i].Canonical = false
		}
	}
	out.BranchCount = len(out.Members)
	out.Unresolved = file.Unresolved
	out.CleanupEligible += file.CleanupEligible
	if len(file.Members) > 0 {
		out.GroupID, out.State = file.GroupID, file.State
	}
	return out
}

func unixMilliOrZero(at time.Time) int64 {
	if at.IsZero() {
		return 0
	}
	return at.UnixMilli()
}

// chooseSessionHead makes one head the conversation's current version. An
// open controller switches in place so its tab keeps identity and path; a
// closed session gets a select marker and lands on the head when reopened.
func (a *App) chooseSessionHead(req RecoveryPreferenceRequest) error {
	path, headID := strings.TrimSpace(req.Path), strings.TrimSpace(req.HeadID)
	if _, _, err := a.sessionDirForPath(path); err != nil {
		return errors.New("session version is unavailable")
	}
	if tab, ctrl := a.tabForSessionPath(path); ctrl != nil {
		if a.tabIsReadOnly(tab) {
			return readOnlyChannelErr()
		}
		if _, err := ctrl.SwitchBranch(headID); err != nil {
			return err
		}
		a.tabMetaAfterHeadSwitch(tab)
	} else if err := agent.SelectSessionHead(path, headID); err != nil {
		return friendlySessionFileError(err)
	}
	dir := filepath.Dir(path)
	if catalog := a.sessionCatalog.Load(); catalog != nil {
		target := sessioncatalog.DirectoryTarget{Path: dir, Scope: req.Scope, WorkspaceRoot: req.WorkspaceRoot}
		if err := catalog.ReconcileDirectory(a.bootContext(), target); err != nil {
			return errors.New("the version choice was saved but the session catalog could not refresh")
		}
	}
	a.emitProjectTreeChangedForSessionDirs(dir)
	return nil
}

// cleanTopicHeads routes cleanup to the heads of a schema-2 log when the
// topic's representative session is one; ok is false for file lineages.
func (a *App) cleanTopicHeads(req RecoveryCleanupRequest, topic sessioncatalog.TopicRecord) (RecoveryCleanupResult, bool) {
	record, ok := topicRecordForLineage(topic, "")
	if !ok {
		return RecoveryCleanupResult{}, false
	}
	heads, err := agent.ListSessionHeads(record.Path)
	if err != nil || len(heads) == 0 {
		return RecoveryCleanupResult{}, false
	}
	return a.cleanSessionHeads(req, record.Path, heads), true
}

// cleanSessionHeads retires covered heads: versions whose whole chain is
// already part of the current one. Diverged heads are never touched, and a
// head active within the quiet period is reported busy rather than retired.
func (a *App) cleanSessionHeads(req RecoveryCleanupRequest, path string, heads []agent.SessionHead) RecoveryCleanupResult {
	result := RecoveryCleanupResult{DryRun: !req.Apply, Items: []RecoveryCleanupItem{}}
	if req.Apply {
		a.sessionRemovalMu.Lock()
		defer a.sessionRemovalMu.Unlock()
	}
	for _, head := range heads {
		if head.Retired || head.Selected || !head.Covered {
			continue
		}
		result.Eligible++
		item := RecoveryCleanupItem{Path: path, HeadID: head.ID, Status: "eligible"}
		if req.Apply {
			item.Status, item.Error = retireCoveredHead(path, head, &result)
		}
		result.Items = append(result.Items, item)
	}
	if result.Moved > 0 {
		a.emitProjectTreeChangedForSessionDirs(filepath.Dir(path))
	}
	return result
}

func retireCoveredHead(path string, head agent.SessionHead, result *RecoveryCleanupResult) (string, string) {
	if time.Since(head.LastActivity) < sessionHeadQuietPeriod {
		result.Busy++
		return "busy", ""
	}
	if err := agent.RetireSessionHead(path, head.ID); err != nil {
		result.Kept++
		return "kept", "session version changed and was kept"
	}
	result.Moved++
	return "retired", ""
}

// RenameSessionHead names one head of a schema-2 session. The main head has
// no name of its own: its note is the session title, so it renames the session.
func (a *App) RenameSessionHead(path, headID, name string) error {
	headID = strings.TrimSpace(headID)
	if headID == "" || headID == agent.SessionMainHead {
		return a.RenameSession(path, name)
	}
	if _, _, err := a.sessionDirForPath(path); err != nil {
		return errors.New("session version is unavailable")
	}
	if err := agent.RenameSessionHead(path, headID, strings.TrimSpace(name)); err != nil {
		return friendlySessionFileError(err)
	}
	a.emitProjectTreeChangedForSessionDirs(filepath.Dir(path))
	return nil
}

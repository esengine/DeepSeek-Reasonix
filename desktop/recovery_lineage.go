package main

import (
	"errors"
	"path/filepath"

	"reasonix/internal/agent"
	"reasonix/internal/control"
	"reasonix/internal/sessioncatalog"
)

type RecoveryLineageMember struct {
	Path           string `json:"path"`
	Role           string `json:"role"`
	Canonical      bool   `json:"canonical"`
	Turns          int    `json:"turns"`
	Open           bool   `json:"open"`
	Running        bool   `json:"running"`
	VersionNote    string `json:"versionNote,omitempty"`
	Preview        string `json:"preview,omitempty"`
	CreatedAt      int64  `json:"createdAt,omitempty"`
	LastActivityAt int64  `json:"lastActivityAt,omitempty"`
}

type RecoveryLineageView struct {
	GroupID         string                  `json:"groupId"`
	State           string                  `json:"state"`
	BranchCount     int                     `json:"branchCount"`
	Unresolved      int                     `json:"unresolved"`
	CleanupEligible int                     `json:"cleanupEligible"`
	Members         []RecoveryLineageMember `json:"members"`
}

type RecoveryCleanupRequest struct {
	Scope         string `json:"scope"`
	WorkspaceRoot string `json:"workspaceRoot,omitempty"`
	TopicID       string `json:"topicId"`
	Apply         bool   `json:"apply"`
}

type RecoveryPreferenceRequest struct {
	Scope         string `json:"scope"`
	WorkspaceRoot string `json:"workspaceRoot,omitempty"`
	TopicID       string `json:"topicId"`
	Path          string `json:"path"`
}

type RecoveryCleanupItem struct {
	Path   string `json:"path"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

type RecoveryCleanupResult struct {
	Eligible int                   `json:"eligible"`
	Moved    int                   `json:"moved"`
	Busy     int                   `json:"busy"`
	Kept     int                   `json:"kept"`
	DryRun   bool                  `json:"dryRun"`
	Items    []RecoveryCleanupItem `json:"items"`
}

func (a *App) GetRecoveryLineage(key ProjectTopicKey) RecoveryLineageView {
	out := RecoveryLineageView{Members: []RecoveryLineageMember{}}
	if a.catalogRebuilding.Load() {
		out.State = "repairing"
		return out
	}
	catalog := a.sessionCatalog.Load()
	if catalog == nil {
		return out
	}
	topic, ok, err := catalog.GetTopic(a.bootContext(), sessioncatalog.TopicKey{Scope: key.Scope, WorkspaceRoot: key.WorkspaceRoot, TopicID: key.TopicID})
	if err != nil || !ok {
		return out
	}
	groupID, directory, ok := recoveryLineageSelection(topic, key.Path)
	if !ok {
		return out
	}
	groups, err := catalog.ListRecoveryGroups(a.bootContext(), directory)
	if err != nil {
		return out
	}
	groupFound := false
	for _, group := range groups {
		if group.ID == groupID {
			out.State = group.State
			groupFound = true
			break
		}
	}
	if !groupFound {
		return RecoveryLineageView{Members: []RecoveryLineageMember{}}
	}
	out.GroupID = groupID
	_, overlays := a.catalogRuntimeOverlays()
	representativeInGroup := false
	for _, record := range topic.Sessions {
		if recoveryRecordBelongsToGroup(record, groupID) && sameRecoveryLineagePath(record.Path, topic.RepresentativePath) {
			representativeInGroup = true
			break
		}
	}
	for _, record := range topic.Sessions {
		if !recoveryRecordBelongsToGroup(record, groupID) {
			continue
		}
		overlay := overlays[sessionRuntimeKey(record.Path)]
		versionNote := record.CustomTitle
		if meta, ok, err := agent.LoadBranchMeta(record.Path); err == nil && ok {
			versionNote = meta.CustomTitle
		}
		canonical := record.RecoveryCanonical
		if representativeInGroup {
			canonical = sameRecoveryLineagePath(record.Path, topic.RepresentativePath)
		}
		out.Members = append(out.Members, RecoveryLineageMember{
			Path: record.Path, Role: record.RecoveryRole, Canonical: canonical,
			Turns: record.Turns, Open: overlay.open, Running: overlay.running,
			VersionNote: versionNote, Preview: record.Preview,
			CreatedAt: record.CreatedAt, LastActivityAt: record.LastActivityAt,
		})
		out.BranchCount++
		if record.RecoveryRole == sessioncatalog.RecoveryRoleDiverged {
			out.Unresolved++
		}
		if record.RecoveryRole == sessioncatalog.RecoveryRoleCoveredCopy {
			out.CleanupEligible++
		}
	}
	if out.State == "" {
		out.State = topic.RecoveryState
	}
	if out.State == "preferred" {
		out.Unresolved = 0
	}
	// The lower-level group API historically calls an all-covered lineage
	// "repairing". Expose its stable state so event consumers can clear pending
	// recovery notifications without polling forever.
	if recoveryLineageIsCovered(out) {
		out.State = "covered"
	}
	if key.RecordClassification {
		recordRecoveryLineageClassification(key.Path, out)
	}
	return out
}

func recordRecoveryLineageClassification(selectedPath string, view RecoveryLineageView) {
	outcome := ""
	switch view.State {
	case "covered", "adopted", "preferred", "diverged":
		outcome = "classified_" + view.State
	default:
		return
	}
	path := ""
	for _, member := range view.Members {
		if sameRecoveryLineagePath(member.Path, selectedPath) {
			path = member.Path
			break
		}
		if path == "" || member.Canonical {
			path = member.Path
		}
	}
	control.RecordRecoveryLifecycle(path, outcome)
}

func recoveryLineageSelection(topic sessioncatalog.TopicRecord, selectedPath string) (string, string, bool) {
	if sessioncatalog.PathIdentityKey(selectedPath) != "" {
		for _, record := range topic.Sessions {
			if !sameRecoveryLineagePath(record.Path, selectedPath) {
				continue
			}
			groupID := record.RecoveryGroupID
			if !record.Recovered {
				groupID = agent.BranchID(record.Path)
			}
			if groupID != "" && recoveryTopicHasGroup(topic, groupID) {
				return groupID, filepath.Dir(record.Path), true
			}
			return "", "", false
		}
		return "", "", false
	}

	groupID, directory := "", ""
	for _, record := range topic.Sessions {
		if !record.Recovered || record.RecoveryGroupID == "" {
			continue
		}
		if groupID != "" && groupID != record.RecoveryGroupID {
			// An older frontend cannot safely choose between multiple groups.
			return "", "", false
		}
		groupID, directory = record.RecoveryGroupID, filepath.Dir(record.Path)
	}
	return groupID, directory, groupID != "" && directory != ""
}

func sameRecoveryLineagePath(left, right string) bool {
	leftKey := sessioncatalog.PathIdentityKey(left)
	return leftKey != "" && leftKey == sessioncatalog.PathIdentityKey(right)
}

func recoveryTopicHasGroup(topic sessioncatalog.TopicRecord, groupID string) bool {
	for _, record := range topic.Sessions {
		if record.Recovered && record.RecoveryGroupID == groupID {
			return true
		}
	}
	return false
}

func recoveryRecordBelongsToGroup(record sessioncatalog.SessionRecord, groupID string) bool {
	if record.Recovered {
		return record.RecoveryGroupID == groupID
	}
	return agent.BranchID(record.Path) == groupID
}

func recoveryLineageIsCovered(view RecoveryLineageView) bool {
	if view.State != "repairing" || view.CleanupEligible == 0 {
		return false
	}
	for _, member := range view.Members {
		if member.Role != sessioncatalog.RecoveryRoleNormal && member.Role != sessioncatalog.RecoveryRoleCoveredCopy {
			return false
		}
	}
	return true
}

// ChooseRecoveryBranch changes only the default open target. Diverged content
// remains on disk and is never made cleanup-eligible by this choice.
func (a *App) ChooseRecoveryBranch(req RecoveryPreferenceRequest) error {
	catalog := a.sessionCatalog.Load()
	if catalog == nil {
		return errors.New("session catalog is unavailable")
	}
	topic, ok, err := catalog.GetTopic(a.bootContext(), sessioncatalog.TopicKey{Scope: req.Scope, WorkspaceRoot: req.WorkspaceRoot, TopicID: req.TopicID})
	if err != nil || !ok {
		return errors.New("recovery lineage is unavailable")
	}
	groupID, dir, ok := recoveryLineageSelection(topic, req.Path)
	if !ok {
		return errors.New("selected branch is outside the recovery lineage")
	}
	groups, err := catalog.ListRecoveryGroups(a.bootContext(), dir)
	if err != nil {
		return errors.New("recovery lineage is unavailable")
	}
	paths := []string{}
	chosen := ""
	foundGroup := false
	for _, group := range groups {
		if group.ID == groupID {
			foundGroup = true
			break
		}
	}
	if !foundGroup {
		return errors.New("recovery lineage is unavailable")
	}
	for _, member := range topic.Sessions {
		if !recoveryRecordBelongsToGroup(member, groupID) {
			continue
		}
		paths = append(paths, member.Path)
		if sameRecoveryLineagePath(member.Path, req.Path) && member.RecoveryRole != sessioncatalog.RecoveryRoleCoveredCopy {
			chosen = member.Path
		}
	}
	if chosen == "" {
		return errors.New("selected branch is outside the recovery lineage")
	}
	defer a.lockRuntimeMutation("choose-recovery-branch")()
	a.sessionRemovalMu.Lock()
	defer a.sessionRemovalMu.Unlock()
	if err := agent.SetRecoveryPreferred(paths, chosen); err != nil {
		return errors.New("could not save the recovery branch choice")
	}
	if err := catalog.ReconcileDirectory(a.bootContext(), sessioncatalog.DirectoryTarget{Path: dir, Scope: req.Scope, WorkspaceRoot: req.WorkspaceRoot}); err != nil {
		return errors.New("the branch choice was saved but the session catalog could not refresh")
	}
	a.emitProjectTreeChangedForSessionDirs(dir)
	return nil
}

// CleanRecoveryLineage performs one backend-owned, revalidated cleanup batch.
// It never purges and never moves diverged content.
func (a *App) CleanRecoveryLineage(req RecoveryCleanupRequest) RecoveryCleanupResult {
	result := RecoveryCleanupResult{DryRun: !req.Apply, Items: []RecoveryCleanupItem{}}
	catalog := a.sessionCatalog.Load()
	if catalog == nil {
		return result
	}
	topic, ok, err := catalog.GetTopic(a.bootContext(), sessioncatalog.TopicKey{Scope: req.Scope, WorkspaceRoot: req.WorkspaceRoot, TopicID: req.TopicID})
	if err != nil || !ok {
		return result
	}
	canonical := ""
	rootID := ""
	for _, record := range topic.Sessions {
		if record.RecoveryCanonical && (record.RecoveryRole == sessioncatalog.RecoveryRoleAdopted || record.RecoveryRole == sessioncatalog.RecoveryRolePreferred) {
			canonical = record.Path
			rootID = record.RecoveryGroupID
			break
		}
	}
	if canonical == "" || rootID == "" {
		return result
	}
	dir := filepath.Dir(canonical)
	groups, err := catalog.ListRecoveryGroups(a.bootContext(), dir)
	if err != nil {
		return result
	}
	members := []sessioncatalog.SessionRecord{}
	for _, group := range groups {
		if group.ID == rootID {
			members = group.Members
			break
		}
	}
	candidates := []sessioncatalog.SessionRecord{}
	for _, record := range members {
		if record.Path == canonical || record.RecoveryRole != sessioncatalog.RecoveryRoleCoveredCopy {
			continue
		}
		result.Eligible++
		candidates = append(candidates, record)
		result.Items = append(result.Items, RecoveryCleanupItem{Path: record.Path, Status: "eligible"})
	}
	if !req.Apply || len(candidates) == 0 {
		return result
	}
	defer a.lockRuntimeMutation("clean-recovery-lineage")()
	a.sessionRemovalMu.Lock()
	defer a.sessionRemovalMu.Unlock()
	if a.sessionOpenInAnyTab(canonical) || agent.SessionLeaseHeld(canonical) {
		for index := range result.Items {
			result.Items[index].Status = "busy"
			result.Busy++
		}
		return result
	}
	if err := agent.ReparentRecoveryCanonical(canonical, rootID, dir); err != nil {
		for index := range result.Items {
			if errors.Is(err, agent.ErrSessionLeaseHeld) {
				result.Items[index].Status = "busy"
				result.Busy++
			} else {
				result.Items[index].Status = "kept"
				result.Items[index].Error = "recovery branch changed and was kept"
				result.Kept++
			}
		}
		return result
	}
	for index, record := range candidates {
		item := &result.Items[index]
		if a.sessionOpenInAnyTab(record.Path) || agent.SessionLeaseHeld(record.Path) {
			item.Status = "busy"
			result.Busy++
			continue
		}
		if err := agent.TrashRecoveryBranchCoveredBy(record.Path, canonical, dir); err != nil {
			item.Status = "kept"
			if errors.Is(err, agent.ErrSessionLeaseHeld) {
				item.Status = "busy"
				result.Busy++
			} else {
				item.Error = "recovery branch changed and was kept"
				result.Kept++
			}
		} else {
			item.Status = "moved"
			result.Moved++
			a.removeSessionCatalogPath(record.Path, "recovery_lineage_cleaned")
		}
	}
	if result.Moved > 0 {
		a.emitProjectTreeChangedForSessionDirs(dir)
		a.invalidatePromptHistoryCache()
	}
	return result
}

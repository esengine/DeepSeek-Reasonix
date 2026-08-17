package main

import (
	"errors"
	"path/filepath"

	"reasonix/internal/agent"
	"reasonix/internal/sessioncatalog"
)

type RecoveryLineageMember struct {
	Path      string `json:"path"`
	Role      string `json:"role"`
	Canonical bool   `json:"canonical"`
	Turns     int    `json:"turns"`
	Open      bool   `json:"open"`
	Running   bool   `json:"running"`
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
	catalog := a.sessionCatalog.Load()
	if catalog == nil {
		return out
	}
	topic, ok, err := catalog.GetTopic(a.bootContext(), sessioncatalog.TopicKey{Scope: key.Scope, WorkspaceRoot: key.WorkspaceRoot, TopicID: key.TopicID})
	if err != nil || !ok {
		return out
	}
	groupID, directory := "", ""
	for _, record := range topic.Sessions {
		if record.Recovered && record.RecoveryGroupID != "" {
			groupID, directory = record.RecoveryGroupID, filepath.Dir(record.Path)
			break
		}
	}
	if groupID == "" || directory == "" {
		return out
	}
	groups, err := catalog.ListRecoveryGroups(a.bootContext(), directory)
	if err != nil {
		return out
	}
	members := []sessioncatalog.SessionRecord{}
	for _, group := range groups {
		if group.ID == groupID {
			members = group.Members
			out.State = group.State
			break
		}
	}
	out.GroupID = groupID
	_, overlays := a.catalogRuntimeOverlays()
	for _, record := range members {
		overlay := overlays[sessionRuntimeKey(record.Path)]
		out.Members = append(out.Members, RecoveryLineageMember{Path: record.Path, Role: record.RecoveryRole,
			Canonical: record.RecoveryCanonical, Turns: record.Turns, Open: overlay.open, Running: overlay.running})
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
	return out
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
	groupID, dir := "", ""
	for _, record := range topic.Sessions {
		if record.Recovered && record.RecoveryGroupID != "" {
			groupID, dir = record.RecoveryGroupID, filepath.Dir(record.Path)
			break
		}
	}
	groups, err := catalog.ListRecoveryGroups(a.bootContext(), dir)
	if err != nil {
		return errors.New("recovery lineage is unavailable")
	}
	paths := []string{}
	chosen := ""
	for _, group := range groups {
		if group.ID != groupID {
			continue
		}
		for _, member := range group.Members {
			paths = append(paths, member.Path)
			if sessionRuntimeKey(member.Path) == sessionRuntimeKey(req.Path) && member.RecoveryRole != sessioncatalog.RecoveryRoleCoveredCopy {
				chosen = member.Path
			}
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

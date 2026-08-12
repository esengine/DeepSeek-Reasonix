package main

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"reasonix/internal/agent"
	"reasonix/internal/sessioncatalog"
)

func (a *App) ResumeSessionPageForTab(tabID, path string, limit int) (HistoryPage, error) {
	tab, ctrl := a.tabAndCtrlByID(tabID)
	if tab == nil || ctrl == nil {
		return HistoryPage{}, fmt.Errorf("tab is not ready")
	}
	resolution := a.resolveCanonicalSession(path)
	if len(resolution.Candidates) > 0 {
		return HistoryPage{SelectionRequired: true, RecoveryCandidates: resolution.Candidates}, nil
	}
	sessionPath, _, err := validateSessionPath(controllerSessionDir(ctrl), resolution.Path)
	if err != nil {
		return HistoryPage{}, err
	}
	loaded, err := loadResumableSession(sessionPath)
	if err != nil {
		return HistoryPage{}, err
	}
	if sessionRuntimeKey(tab.currentSessionPath()) != sessionRuntimeKey(sessionPath) {
		if err := a.rebindTabToLoadedSessionPath(tab, sessionPath, loaded); err != nil {
			return HistoryPage{}, err
		}
	}
	a.setTabReadOnly(tab.ID, false)
	page := a.HistoryPageForTab(tab.ID, 0, limit)
	page.ResolvedPath, page.Redirected = sessionPath, resolution.Redirected
	return page, nil
}

// ResumeRecoveryCandidatePageForTab confirms a candidate without binding an
// old or unrelated path while the chooser is open.
func (a *App) ResumeRecoveryCandidatePageForTab(tabID, originalPath, selectedPath string, limit int) (HistoryPage, error) {
	confirmedPath, err := a.ConfirmRecoverySessionCandidate(originalPath, selectedPath)
	if err != nil {
		return HistoryPage{}, err
	}
	selectedPath = confirmedPath
	tab, ctrl := a.tabAndCtrlByID(tabID)
	if tab == nil || ctrl == nil {
		return HistoryPage{}, fmt.Errorf("tab is not ready")
	}
	sessionPath, _, err := validateSessionPath(controllerSessionDir(ctrl), selectedPath)
	if err != nil {
		return HistoryPage{}, err
	}
	loaded, err := loadResumableSession(sessionPath)
	if err != nil {
		return HistoryPage{}, err
	}
	if sessionRuntimeKey(tab.currentSessionPath()) != sessionRuntimeKey(sessionPath) {
		if err := a.rebindTabToLoadedSessionPath(tab, sessionPath, loaded); err != nil {
			return HistoryPage{}, err
		}
	}
	a.setTabReadOnly(tab.ID, false)
	page := a.HistoryPageForTab(tab.ID, 0, limit)
	page.ResolvedPath = sessionPath
	return page, nil
}

type canonicalSessionResolution struct {
	Path       string
	Redirected bool
	Candidates []RecoverySessionCandidate
}

type RecoverySessionCandidate struct {
	Path           string `json:"path"`
	LastActivityAt int64  `json:"lastActivityAt"`
	Summary        string `json:"summary"`
	Turns          int    `json:"turns"`
}

type RecoverySessionResolution struct {
	Path               string                     `json:"path"`
	Redirected         bool                       `json:"redirected,omitempty"`
	SelectionRequired  bool                       `json:"selectionRequired,omitempty"`
	RecoveryCandidates []RecoverySessionCandidate `json:"recoveryCandidates,omitempty"`
}

func (a *App) ResolveRecoverySession(path string) RecoverySessionResolution {
	resolution := a.resolveCanonicalSession(path)
	return RecoverySessionResolution{
		Path: resolution.Path, Redirected: resolution.Redirected,
		SelectionRequired:  len(resolution.Candidates) > 0,
		RecoveryCandidates: resolution.Candidates,
	}
}

func (a *App) ConfirmRecoverySessionCandidate(originalPath, selectedPath string) (string, error) {
	resolution := a.resolveCanonicalSession(originalPath)
	for _, candidate := range resolution.Candidates {
		if sameDesktopPath(candidate.Path, selectedPath) {
			return candidate.Path, nil
		}
	}
	return "", fmt.Errorf("recovery candidate is no longer available")
}

// resolveCanonicalSessionPath returns a unique adopted/canonical leaf for the
// topic that owns path, when the catalog has one. Empty means keep path.
// Retarget happens before Controller create/rebind so the new controller leases
// and binds authority on the canonical path only.
func (a *App) resolveCanonicalSessionPath(path string) string {
	resolution := a.resolveCanonicalSession(path)
	if resolution.Redirected {
		return resolution.Path
	}
	return ""
}

func (a *App) resolveCanonicalSession(path string) canonicalSessionResolution {
	if a == nil || strings.TrimSpace(path) == "" {
		return canonicalSessionResolution{Path: path}
	}
	catalog := a.sessionCatalog.Load()
	if catalog == nil {
		return canonicalSessionResolution{Path: path}
	}
	ctx := context.Background()
	rec, ok, err := catalog.GetSession(ctx, path)
	if err != nil || !ok {
		return canonicalSessionResolution{Path: path}
	}
	groupID := strings.TrimSpace(rec.RecoveryGroupID)
	if groupID == "" {
		groupID = agent.BranchID(rec.Path)
	}
	related := []sessioncatalog.SessionRecord{}
	if rec.TopicID != "" {
		topic, found, topicErr := catalog.GetTopic(ctx, sessioncatalog.TopicKey{
			Scope: rec.Scope, WorkspaceRoot: rec.WorkspaceRoot, TopicID: rec.TopicID,
		})
		if topicErr != nil || !found {
			return canonicalSessionResolution{Path: path}
		}
		for _, member := range topic.Sessions {
			if member.RecoveryGroupID == groupID || sameDesktopPath(member.Path, rec.Path) {
				related = append(related, member)
			}
		}
	} else {
		groups, groupErr := catalog.ListRecoveryGroups(ctx, rec.Directory)
		if groupErr != nil {
			return canonicalSessionResolution{Path: path}
		}
		for _, group := range groups {
			if group.ID == groupID {
				related = append(related, group.Members...)
				break
			}
		}
	}
	if canonical := sessioncatalog.CanonicalSessionPathForTopic(related, path); canonical != "" {
		return canonicalSessionResolution{Path: canonical, Redirected: true}
	}
	if rec.RecoveryCanonical && rec.RecoveryRole == sessioncatalog.RecoveryRoleAdopted {
		return canonicalSessionResolution{Path: path}
	}
	var candidates []RecoverySessionCandidate
	for _, s := range related {
		if !s.Recovered || s.RecoveryRole != sessioncatalog.RecoveryRoleDiverged {
			continue
		}
		candidates = append(candidates, RecoverySessionCandidate{
			Path: s.Path, LastActivityAt: s.LastActivityAt, Summary: recoverySessionLatestSummary(s.Path), Turns: s.Turns,
		})
	}
	if len(candidates) < 2 {
		return canonicalSessionResolution{Path: path}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].LastActivityAt != candidates[j].LastActivityAt {
			return candidates[i].LastActivityAt > candidates[j].LastActivityAt
		}
		return candidates[i].Path < candidates[j].Path
	})
	return canonicalSessionResolution{Path: path, Candidates: candidates}
}

func recoverySessionLatestSummary(path string) string {
	session, err := agent.LoadSession(path)
	if err != nil {
		return ""
	}
	messages := session.Snapshot()
	for i := len(messages) - 1; i >= 0; i-- {
		text := strings.TrimSpace(messages[i].Content)
		if text == "" {
			continue
		}
		runes := []rune(text)
		if len(runes) > 240 {
			text = string(runes[:240]) + "..."
		}
		return text
	}
	return ""
}

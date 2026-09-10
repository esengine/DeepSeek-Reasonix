package main

import (
	"fmt"
	"strings"

	"reasonix/internal/sessioncatalog"
)

// preferLiveSessionPath chooses which session a topic row should open.
// A live runtime wins over ordinary catalog rows (RepresentativePath,
// canonical leaf, and the covering parent that continue would follow).
// An explicit non-ordinary path (History inspect) is left alone.
func preferLiveSessionPath(requested, live string, ordinary ...string) string {
	liveKey := sessionRuntimeKey(live)
	if liveKey == "" {
		return requested
	}
	reqKey := sessionRuntimeKey(requested)
	if reqKey == "" || reqKey == liveKey {
		if reqKey == "" {
			return live
		}
		return requested
	}
	if len(ordinary) == 0 {
		return live
	}
	for _, path := range ordinary {
		if sessionRuntimeKey(path) == reqKey {
			return live
		}
	}
	return requested
}

func (a *App) liveSessionPathForTopic(scope, workspaceRoot, topicID string) string {
	if a == nil {
		return ""
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	if tab := a.liveRuntimeForTopicLocked(scope, workspaceRoot, topicID); tab != nil {
		return tab.currentSessionPath()
	}
	return ""
}

func (a *App) liveRuntimeForTopicLocked(scope, workspaceRoot, topicID string) *WorkspaceTab {
	var best *WorkspaceTab
	visit := func(tab *WorkspaceTab) {
		if tab == nil || !tabMatchesTopicTarget(tab, scope, workspaceRoot, topicID) {
			return
		}
		if tab.currentSessionPath() == "" {
			return
		}
		if best == nil {
			best = tab
			return
		}
		if best.Ctrl == nil && tab.Ctrl != nil {
			best = tab
			return
		}
		if normalizeTopicStatus(best.ActivityStatus) == "" && normalizeTopicStatus(tab.ActivityStatus) != "" {
			best = tab
		}
	}
	for _, tab := range a.tabs {
		visit(tab)
	}
	for _, tab := range a.detachedSessions {
		visit(tab)
	}
	return best
}

func (a *App) resolveOpenSessionPath(scope, workspaceRoot, topicID, requested string) string {
	live := a.liveSessionPathForTopic(scope, workspaceRoot, topicID)
	return preferLiveSessionPath(requested, live, a.ordinaryOpenSessionPaths(scope, workspaceRoot, topicID, requested)...)
}

func (a *App) catalogTopicRecord(scope, workspaceRoot, topicID string) (sessioncatalog.TopicRecord, bool) {
	if a == nil || strings.TrimSpace(topicID) == "" {
		return sessioncatalog.TopicRecord{}, false
	}
	catalog := a.sessionCatalog.Load()
	if catalog == nil {
		return sessioncatalog.TopicRecord{}, false
	}
	topic, ok, err := catalog.GetTopic(a.bootContext(), sessioncatalog.TopicKey{
		Scope: scope, WorkspaceRoot: workspaceRoot, TopicID: topicID,
	})
	if err != nil || !ok {
		return sessioncatalog.TopicRecord{}, false
	}
	return topic, true
}

// ordinaryOpenSessionPaths are the catalog identities of the sidebar row:
// RepresentativePath, the canonical covering leaf, and the parent that
// continue would follow. History inspect paths are not in this set.
func (a *App) ordinaryOpenSessionPaths(scope, workspaceRoot, topicID, requested string) []string {
	var ordinary []string
	if topic, ok := a.catalogTopicRecord(scope, workspaceRoot, topicID); ok {
		if rep := strings.TrimSpace(topic.RepresentativePath); rep != "" {
			ordinary = append(ordinary, rep)
		}
		if canonical := sessioncatalog.CanonicalSessionPathForTopic(topic.Sessions, ""); canonical != "" {
			ordinary = append(ordinary, canonical)
		}
	} else if path := a.catalogSessionPathForTopic(scope, workspaceRoot, topicID); path != "" {
		ordinary = append(ordinary, path)
	}
	if continued := a.continuePathForOpen(requested); continued != "" {
		ordinary = append(ordinary, requested, continued)
	}
	return ordinary
}

func (a *App) openTopicTabPreferLiveActivation(scope, workspaceRoot, topicID, sessionPath string, activate bool) (TabMeta, error) {
	sessionPath = a.resolveOpenSessionPath(scope, workspaceRoot, topicID, sessionPath)
	a.mu.Lock()
	if promoted := a.promoteDetachedRuntimeLocked(sessionPath, activate); promoted != nil {
		meta := a.tabMeta(promoted, promoted.ID == a.activeTabID)
		a.saveTabsLocked()
		a.mu.Unlock()
		// A TurnDone missed while the runtime was detached leaves a permanent
		// spinner; reconcile against the controller's real turn state on open.
		a.reconcileTabActivityStatus(promoted)
		a.emitProjectTreeRuntimeChangedWithLegacy()
		return enrichTabMeta(meta), nil
	}
	a.mu.Unlock()
	return a.openTopicTabWithActivation(scope, workspaceRoot, topicID, sessionPath, activate)
}

func (a *App) promoteDetachedRuntimeLocked(sessionPath string, activate bool) *WorkspaceTab {
	key := sessionRuntimeKey(sessionPath)
	if key == "" {
		return nil
	}
	tab := a.detachedSessions[key]
	if tab == nil || tab.Ctrl == nil {
		return nil
	}
	delete(a.detachedSessions, key)
	if a.tabs[tab.ID] == nil {
		a.tabs[tab.ID] = tab
		a.tabOrder = append(a.tabOrder, tab.ID)
	}
	if activate {
		a.activeTabID = tab.ID
	}
	return tab
}

// renameCatalogOnlyTopic persists a rename for a topic known only to the
// session catalog (metadata shell, no title map entry, no open tab) through
// the same title metadata path as an indexed topic, instead of failing the
// rename (#9090). It returns the usual "not found" error when the catalog
// does not list the topic either.
func (a *App) renameCatalogOnlyTopic(topicID, title string) error {
	scope, workspaceRoot, ok := a.findCatalogTopicLocation(topicID)
	if !ok {
		return fmt.Errorf("topic %q not found", topicID)
	}
	if scope == "global" {
		workspaceRoot = ""
	}
	if err := setTopicTitle(workspaceRoot, topicID, title); err != nil {
		return err
	}
	changedDirs := a.updateTopicSessionTitles(topicID, title)
	if len(changedDirs) > 0 {
		a.emitProjectTreeChangedForSessionDirs(changedDirs...)
	} else {
		a.emitProjectTreeMetadataChanged()
	}
	return nil
}

// findCatalogTopicLocation resolves a topic that exists only in the session
// catalog to its scope and workspace root.
func (a *App) findCatalogTopicLocation(topicID string) (string, string, bool) {
	topicID = strings.TrimSpace(topicID)
	if topicID == "" {
		return "", "", false
	}
	catalog := a.sessionCatalog.Load()
	if catalog == nil {
		return "", "", false
	}
	ctx, cancel := a.catalogReadContext()
	defer cancel()
	if _, ok, err := catalog.GetTopic(ctx, sessioncatalog.TopicKey{Scope: "global", TopicID: topicID}); err == nil && ok {
		return "global", "", true
	}
	for _, p := range loadProjectsFile().Projects {
		root := normalizeProjectRoot(p.Root)
		if root == "" {
			continue
		}
		if _, ok, err := catalog.GetTopic(ctx, sessioncatalog.TopicKey{Scope: "project", WorkspaceRoot: root, TopicID: topicID}); err == nil && ok {
			return "project", root, true
		}
	}
	return "", "", false
}

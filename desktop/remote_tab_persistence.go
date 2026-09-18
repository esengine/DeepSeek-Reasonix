package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
)

type desktopRemoteTabEntry struct {
	ID           string `json:"id"`
	HostID       string `json:"hostId"`
	Workspace    string `json:"workspace"`
	TopicTitle   string `json:"topicTitle,omitempty"`
	Model        string `json:"model,omitempty"`
	SessionName  string `json:"sessionName,omitempty"`
	SessionPath  string `json:"sessionPath,omitempty"`
	SessionID    string `json:"sessionId,omitempty"`
	SessionReset bool   `json:"sessionReset,omitempty"`
	extra        map[string]json.RawMessage
}

func singleSurfaceTabsFile(f desktopTabsFile) desktopTabsFile {
	if len(f.Tabs)+len(f.RemoteTabs) <= 1 {
		return f
	}
	active := strings.TrimSpace(f.ActiveTab)
	for _, entry := range f.RemoteTabs {
		if entry.ID != active {
			continue
		}
		// The remote surface stays active, but one local entry is kept so local
		// commands still have a workspace tab to target. It restores dormant
		// (see restoreDormantWorkspaceTab), so it adds no hidden startup work.
		if len(f.Tabs) == 0 {
			return desktopTabsFile{
				ActiveTab:      entry.ID,
				RemoteTabs:     []desktopRemoteTabEntry{entry},
				RemoteTabOrder: []string{entry.ID},
				TabOrder:       []string{entry.ID},
				extra:          cloneDesktopJSONFields(f.extra),
			}
		}
		local := f.Tabs[0]
		return desktopTabsFile{
			Tabs:           []desktopTabEntry{local},
			ActiveTab:      entry.ID,
			RemoteTabs:     []desktopRemoteTabEntry{entry},
			RemoteTabOrder: []string{entry.ID},
			// The remote surface keeps the head of the strip order so any
			// first-tab fallback still resolves to the surface the user left on.
			TabOrder: []string{entry.ID, local.ID},
		}
	}
	for _, entry := range f.Tabs {
		if entry.ID == active {
			return desktopTabsFile{Tabs: []desktopTabEntry{entry}, ActiveTab: entry.ID, TabOrder: []string{entry.ID}, extra: cloneDesktopJSONFields(f.extra)}
		}
	}
	if len(f.Tabs) > 0 {
		return desktopTabsFile{Tabs: []desktopTabEntry{f.Tabs[0]}, ActiveTab: f.Tabs[0].ID, TabOrder: []string{f.Tabs[0].ID}, extra: cloneDesktopJSONFields(f.extra)}
	}
	chosen := f.RemoteTabs[0]
	return desktopTabsFile{ActiveTab: chosen.ID, RemoteTabs: []desktopRemoteTabEntry{chosen}, RemoteTabOrder: []string{chosen.ID}, TabOrder: []string{chosen.ID}, extra: cloneDesktopJSONFields(f.extra)}
}

// remoteSurfaceIsActiveTab reports whether the persisted active tab is a remote
// entry: the remote shell then owns the visible surface, and restored local tabs
// stay dormant (no runtime) so they add no hidden startup work.
func remoteSurfaceIsActiveTab(f desktopTabsFile) bool {
	active := strings.TrimSpace(f.ActiveTab)
	if active == "" {
		return false
	}
	for _, entry := range f.RemoteTabs {
		if strings.TrimSpace(entry.ID) == active {
			return true
		}
	}
	return false
}

// saveTabsFromRemote snapshots local state before joining it with the remote
// registry. Callers must not hold remoteTabMu.
func (a *App) saveTabsFromRemote() {
	a.mu.Lock()
	dir, entries, activeID, version := a.saveTabsCollectLocked()
	a.mu.Unlock()
	a.saveTabsWrite(dir, entries, activeID, version)
}

// finishRestoredLocalTabs activates and builds the restored local tabs. When the
// persisted active tab is a remote shell they stay dormant instead: the remote
// surface keeps the visible surface and no hidden startup work is added. Their
// controller is built when they are first activated or used.
func (a *App) finishRestoredLocalTabs(f desktopTabsFile, toBuild []*WorkspaceTab) {
	if remoteSurfaceIsActiveTab(f) {
		return
	}
	a.mu.Lock()
	if _, ok := a.tabs[f.ActiveTab]; ok {
		a.activeTabID = f.ActiveTab
	} else if ordered := a.orderedTabIDsLocked(); len(ordered) > 0 {
		a.activeTabID = ordered[0]
	}
	a.saveTabsLocked()
	a.mu.Unlock()
	for _, tab := range toBuild {
		a.startTabControllerBuild(tab)
	}
}

// restoreDormantWorkspaceTab publishes one local workspace tab with no session
// and no runtime. A remote-only layout leaves the visible surface to the remote
// shell, but local commands still need a tab to target; this tab is activated
// on first use and never takes the surface from the remote tab.
func (a *App) restoreDormantWorkspaceTab(ctx context.Context) {
	admissionRelease, err := a.beginProjectRuntimeAdmission("global", globalTabWorkspaceRoot())
	if err != nil {
		slog.Warn("desktop: dormant workspace tab admission failed", "err", err)
		return
	}
	tab := a.createTabEntry("global", globalTabWorkspaceRoot(), "")
	tab.sink = &tabEventSink{tabID: tab.ID, app: a, ctx: ctx}
	a.publishRestoredTab(tab, admissionRelease)
}

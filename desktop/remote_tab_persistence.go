package main

import "strings"

type desktopRemoteTabEntry struct {
	ID           string `json:"id"`
	HostID       string `json:"hostId"`
	Workspace    string `json:"workspace"`
	TopicTitle   string `json:"topicTitle,omitempty"`
	Model        string `json:"model,omitempty"`
	SessionName  string `json:"sessionName,omitempty"`
	SessionPath  string `json:"sessionPath,omitempty"`
	SessionReset bool   `json:"sessionReset,omitempty"`
}

func singleSurfaceLayoutStyle(style string) bool {
	switch strings.ToLower(strings.TrimSpace(style)) {
	case "workbench", "creation":
		return true
	default:
		return false
	}
}

func singleSurfaceTabsFile(f desktopTabsFile) desktopTabsFile {
	if len(f.Tabs)+len(f.RemoteTabs) <= 1 {
		return f
	}
	active := strings.TrimSpace(f.ActiveTab)
	for _, entry := range f.RemoteTabs {
		if entry.ID == active {
			return desktopTabsFile{
				ActiveTab:      entry.ID,
				RemoteTabs:     []desktopRemoteTabEntry{entry},
				RemoteTabOrder: []string{entry.ID},
				TabOrder:       []string{entry.ID},
			}
		}
	}
	for _, entry := range f.Tabs {
		if entry.ID == active {
			return desktopTabsFile{Tabs: []desktopTabEntry{entry}, ActiveTab: entry.ID, TabOrder: []string{entry.ID}}
		}
	}
	if len(f.Tabs) > 0 {
		return desktopTabsFile{Tabs: []desktopTabEntry{f.Tabs[0]}, ActiveTab: f.Tabs[0].ID, TabOrder: []string{f.Tabs[0].ID}}
	}
	chosen := f.RemoteTabs[0]
	return desktopTabsFile{ActiveTab: chosen.ID, RemoteTabs: []desktopRemoteTabEntry{chosen}, RemoteTabOrder: []string{chosen.ID}, TabOrder: []string{chosen.ID}}
}

// saveTabsFromRemote snapshots local state before joining it with the remote
// registry. Callers must not hold remoteTabMu.
func (a *App) saveTabsFromRemote() {
	a.mu.Lock()
	dir, entries, activeID, version := a.saveTabsCollectLocked()
	a.mu.Unlock()
	a.saveTabsWrite(dir, entries, activeID, version)
}

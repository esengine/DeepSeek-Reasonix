package main

import (
	"encoding/json"
	"fmt"
	"log"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"reasonix/internal/config"
	"reasonix/internal/fileutil"
)

var remotePrefsMu sync.Mutex

// remotePrefs is desktop-only remote UI state, stored beside the other desktop
// JSON prefs (desktop-workspaces.json, desktop-tabs.json). All fields are
// optional so an older file decodes cleanly.
type remotePrefs struct {
	LastHostID          string            `json:"lastHostId,omitempty"`
	LastWorkspaceByHost map[string]string `json:"lastWorkspaceByHost,omitempty"`
	ExplorerTab         string            `json:"explorerTab,omitempty"`
	SessionTitles       map[string]string `json:"sessionTitles,omitempty"`
	PinnedSessions      []string          `json:"pinnedSessions,omitempty"`
	// CredentialProxySecret is the random root of the per-host virtual tokens
	// used by local-proxy credential mode. Rotating it revokes every token.
	CredentialProxySecret string `json:"credentialProxySecret,omitempty"`
}

func remotePrefsPath() string {
	dir := config.MemoryUserDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "desktop-remote.json")
}

func loadRemotePrefs() remotePrefs {
	p := remotePrefs{
		LastWorkspaceByHost: map[string]string{},
		SessionTitles:       map[string]string{},
	}
	path := remotePrefsPath()
	if path == "" {
		// Keep the "maps are always initialized" contract on this early
		// return too: writers assign into them without a nil guard.
		p.LastWorkspaceByHost = map[string]string{}
		p.SessionTitles = map[string]string{}
		return p
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return p
	}
	_ = json.Unmarshal(data, &p)
	if p.LastWorkspaceByHost == nil {
		p.LastWorkspaceByHost = map[string]string{}
	}
	if p.SessionTitles == nil {
		p.SessionTitles = map[string]string{}
	}
	return p
}

func remotePrefsForUpdateLocked() remotePrefs {
	p := loadRemotePrefs()
	p.LastWorkspaceByHost = cloneStringMap(p.LastWorkspaceByHost)
	p.SessionTitles = cloneStringMap(p.SessionTitles)
	p.PinnedSessions = append([]string(nil), p.PinnedSessions...)
	return p
}

// remotePrefsSnapshot amortizes reads for one listing operation without
// retaining state across calls or desktop instances.
func remotePrefsSnapshot() remotePrefs {
	remotePrefsMu.Lock()
	defer remotePrefsMu.Unlock()
	p := loadRemotePrefs()
	p.LastWorkspaceByHost = cloneStringMap(p.LastWorkspaceByHost)
	p.SessionTitles = cloneStringMap(p.SessionTitles)
	p.PinnedSessions = append([]string(nil), p.PinnedSessions...)
	return p
}

func cloneStringMap(src map[string]string) map[string]string {
	dst := make(map[string]string, len(src))
	maps.Copy(dst, src)
	return dst
}

func remotePrefsSaveLocked(p remotePrefs) error {
	if err := saveRemotePrefs(p); err != nil {
		log.Printf("[remote] prefs: save FAILED err=%v", err)
		return err
	}
	return nil
}

// updateRemotePrefsLocked serializes the full read-modify-write transaction
// with other Reasonix processes. Callers already hold remotePrefsMu, which
// provides the equivalent process-local ordering.
func updateRemotePrefsLocked(mutate func(*remotePrefs) (bool, error)) (remotePrefs, error) {
	path := remotePrefsPath()
	if strings.TrimSpace(path) == "" {
		return remotePrefs{}, fmt.Errorf("remote prefs: no user dir")
	}
	unlock, err := config.LockConfigFileEdits(path)
	if err != nil {
		return remotePrefs{}, fmt.Errorf("remote prefs: lock: %w", err)
	}
	defer unlock()
	p := remotePrefsForUpdateLocked()
	changed, err := mutate(&p)
	if err != nil {
		return remotePrefs{}, err
	}
	if changed {
		if err := remotePrefsSaveLocked(p); err != nil {
			return remotePrefs{}, err
		}
	}
	return p, nil
}

func remoteSessionPrefKey(hostID, workspace, name string) string {
	return hostID + "\x00" + workspace + "\x00" + name
}

func remoteSessionTitleOverride(hostID, workspace, name string) string {
	remotePrefsMu.Lock()
	defer remotePrefsMu.Unlock()
	return loadRemotePrefs().SessionTitles[remoteSessionPrefKey(hostID, workspace, name)]
}

func remoteSessionPinned(hostID, workspace, name string) bool {
	remotePrefsMu.Lock()
	defer remotePrefsMu.Unlock()
	return remoteSessionPinnedLocked(loadRemotePrefs(), remoteSessionPrefKey(hostID, workspace, name))
}

func remoteSessionPinnedLocked(p remotePrefs, key string) bool {
	return slices.Contains(p.PinnedSessions, key)
}

func setRemoteSessionPinned(hostID, workspace, name string, pinned bool) error {
	remotePrefsMu.Lock()
	defer remotePrefsMu.Unlock()
	_, err := updateRemotePrefsLocked(func(p *remotePrefs) (bool, error) {
		key := remoteSessionPrefKey(hostID, workspace, name)
		if remoteSessionPinnedLocked(*p, key) == pinned {
			return false, nil
		}
		next := make([]string, 0, len(p.PinnedSessions)+1)
		for _, existing := range p.PinnedSessions {
			if existing != key {
				next = append(next, existing)
			}
		}
		if pinned {
			next = append(next, key)
		}
		p.PinnedSessions = next
		return true, nil
	})
	return err
}

func setRemoteSessionTitleOverride(hostID, workspace, name, title string) error {
	remotePrefsMu.Lock()
	defer remotePrefsMu.Unlock()
	_, err := updateRemotePrefsLocked(func(p *remotePrefs) (bool, error) {
		key := remoteSessionPrefKey(hostID, workspace, name)
		title = strings.TrimSpace(title)
		if p.SessionTitles[key] == title {
			return false, nil
		}
		if title == "" {
			delete(p.SessionTitles, key)
		} else {
			p.SessionTitles[key] = title
		}
		return true, nil
	})
	return err
}

// migrateRemoteSessionTitleOverride moves preferences assigned to the
// synthetic blank row onto the durable Serve session name created by its first
// turn. Keeping title and pin migration in one locked update prevents a
// listing refresh from observing either preference under neither identity.
func migrateRemoteSessionTitleOverride(hostID, workspace, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", nil
	}
	remotePrefsMu.Lock()
	defer remotePrefsMu.Unlock()
	named := ""
	_, err := updateRemotePrefsLocked(func(p *remotePrefs) (bool, error) {
		blankKey := remoteSessionPrefKey(hostID, workspace, "")
		namedKey := remoteSessionPrefKey(hostID, workspace, name)
		named = strings.TrimSpace(p.SessionTitles[namedKey])
		blank := strings.TrimSpace(p.SessionTitles[blankKey])
		changed := false
		if named == "" && blank != "" {
			named = blank
			p.SessionTitles[namedKey] = blank
			changed = true
		}
		if blank != "" {
			delete(p.SessionTitles, blankKey)
			changed = true
		}
		if remoteSessionPinnedLocked(*p, blankKey) {
			next := make([]string, 0, len(p.PinnedSessions))
			for _, key := range p.PinnedSessions {
				if key != blankKey && key != namedKey {
					next = append(next, key)
				}
			}
			next = append(next, namedKey)
			p.PinnedSessions = next
			changed = true
		}
		return changed, nil
	})
	return named, err
}

func saveRemotePrefs(p remotePrefs) error {
	path := remotePrefsPath()
	if path == "" {
		return fmt.Errorf("remote prefs: no user dir")
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return fmt.Errorf("remote prefs: encode: %w", err)
	}
	return fileutil.AtomicWriteFile(path, data, 0o600)
}

func (a *App) saveLastRemoteWorkspace(hostID, workspace string) error {
	remotePrefsMu.Lock()
	defer remotePrefsMu.Unlock()
	_, err := updateRemotePrefsLocked(func(p *remotePrefs) (bool, error) {
		if p.LastHostID == hostID && p.LastWorkspaceByHost[hostID] == workspace {
			return false, nil
		}
		p.LastHostID = hostID
		p.LastWorkspaceByHost[hostID] = workspace
		return true, nil
	})
	return err
}

// RemoteLastWorkspace returns the last opened workspace for hostID (bound so
// the frontend can prefill the server card).
func (a *App) RemoteLastWorkspace(hostID string) string {
	remotePrefsMu.Lock()
	defer remotePrefsMu.Unlock()
	return loadRemotePrefs().LastWorkspaceByHost[hostID]
}

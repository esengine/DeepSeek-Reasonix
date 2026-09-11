package main

import (
	"fmt"
	"path"
	"strings"

	"reasonix/internal/config"
	"reasonix/internal/store"
)

// ListRemoteProjects returns every pinned remote workspace in config order.
func (a *App) ListRemoteProjects() ([]RemoteProjectView, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	out := make([]RemoteProjectView, 0, len(cfg.Remote.Projects))
	for _, p := range cfg.Remote.Projects {
		out = append(out, remoteProjectEntryToView(p))
	}
	return out, nil
}

func (a *App) AddRemoteProject(hostID, workspace string) (RemoteProjectView, error) {
	hostID = strings.TrimSpace(hostID)
	workspace = strings.TrimSpace(workspace)
	var view RemoteProjectView
	err := editUserConfigIfChanged(func(c *config.Config) (bool, error) {
		// Overlapping pins on one host collapse into the existing group.
		// This avoids duplicate serves over the same files and returns the
		// canonical workspace with Merged set.
		if merged, ok := resolveOverlappingWorkspace(c.Remote.Projects, hostID, workspace); ok {
			workspace = merged
			stored, retained := c.RemoteProject(hostID, merged)
			if !retained {
				return false, fmt.Errorf("remote project was not retained")
			}
			view = remoteProjectEntryToView(stored)
			view.Merged = true
			return false, nil
		}
		entry := config.RemoteProjectEntry{
			HostID:    hostID,
			Workspace: workspace,
		}
		if err := c.UpsertRemoteProject(entry); err != nil {
			return false, err
		}
		stored, ok := c.RemoteProject(entry.HostID, entry.Workspace)
		if !ok {
			return false, fmt.Errorf("remote project was not retained")
		}
		view = remoteProjectEntryToView(stored)
		return true, nil
	})
	if err != nil {
		return RemoteProjectView{}, err
	}
	return view, nil
}

// resolveOverlappingWorkspace finds the existing pin on the same host that the
// requested workspace should merge into: an exact match wins, then the
// nearest ancestor pin, then the shallowest descendant pin. Remote paths are
// POSIX; "~" and unresolvable relatives simply never overlap (safe default).
func resolveOverlappingWorkspace(existing []config.RemoteProjectEntry, hostID, workspace string) (string, bool) {
	target := cleanRemoteWorkspace(workspace)
	if target == "" {
		return "", false
	}
	ancestor, ancestorDepth := "", -1
	descendant, descendantDepth := "", 1<<30
	for _, p := range existing {
		if p.HostID != hostID {
			continue
		}
		cand := cleanRemoteWorkspace(p.Workspace)
		if cand == "" {
			continue
		}
		switch {
		case cand == target:
			return p.Workspace, true
		case isRemoteSubpath(cand, target): // existing pin is an ancestor of the request
			if d := pathDepth(cand); ancestor == "" || d > ancestorDepth {
				ancestor, ancestorDepth = p.Workspace, d
			}
		case isRemoteSubpath(target, cand): // existing pin is a descendant of the request
			if d := pathDepth(cand); descendant == "" || d < descendantDepth {
				descendant, descendantDepth = p.Workspace, d
			}
		}
	}
	if ancestor != "" {
		return ancestor, true
	}
	return descendant, descendant != ""
}

func cleanRemoteWorkspace(ws string) string {
	ws = strings.TrimSpace(ws)
	if ws == "" || ws == "~" {
		return ws
	}
	return path.Clean(strings.TrimRight(ws, "/"))
}

// isRemoteSubpath reports parent/child nesting between two cleaned POSIX
// paths; equal paths are deliberately not subpaths of each other.
func isRemoteSubpath(parent, child string) bool {
	if parent == "/" {
		return strings.HasPrefix(child, "/") && child != "/"
	}
	return strings.HasPrefix(child, parent+"/")
}

func pathDepth(cleaned string) int {
	if cleaned == "" || cleaned == "/" {
		return 0
	}
	return strings.Count(cleaned, "/")
}

func (a *App) RemoveRemoteProject(hostID, workspace string) error {
	return editUserConfig(func(c *config.Config) error {
		c.RemoveRemoteProject(strings.TrimSpace(hostID), strings.TrimSpace(workspace))
		return nil
	})
}

func remoteProjectEntryToView(p config.RemoteProjectEntry) RemoteProjectView {
	return RemoteProjectView{HostID: p.HostID, Workspace: p.Workspace, Title: p.Title}
}

// remoteProjectNodes lists pinned remote workspaces as project group shells
// for the tree snapshot. Read failures degrade to "no remote projects" at the
// caller — a broken config must not take the whole tree down.
func (a *App) remoteProjectNodes() ([]ProjectNode, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	out := make([]ProjectNode, 0, len(cfg.Remote.Projects))
	for _, p := range cfg.Remote.Projects {
		label := strings.TrimSpace(p.Title)
		if label == "" {
			label = remoteWorkspaceName(p.Workspace)
		}
		out = append(out, ProjectNode{
			Key:   "project_remote_" + store.RemoteWorkspaceSlug(p.HostID+":"+p.Workspace),
			Kind:  "project",
			Label: label,
			// Root participates in tree selection and drag identity. Qualify it
			// with the host so identical paths on two hosts never alias.
			Root:   "remote-project:" + p.HostID + ":" + p.Workspace,
			Remote: &RemoteTabRef{HostID: p.HostID, Workspace: p.Workspace},
		})
	}
	return out, nil
}

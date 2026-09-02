package main

import (
	"context"
	"path/filepath"
	"strings"

	"reasonix/internal/config"
	"reasonix/internal/sessioncatalog"
)

func (a *App) sessionCatalogTargets() []sessioncatalog.DirectoryTarget {
	f := loadProjectsFile()
	out := []sessioncatalog.DirectoryTarget{}
	add := func(target sessioncatalog.DirectoryTarget) {
		out = append(out, target)
	}
	add(sessioncatalog.DirectoryTarget{Path: config.SessionDir(), Scope: "global"})
	add(sessioncatalog.DirectoryTarget{Path: desktopSessionDir(globalWorkspaceRoot()), Scope: "global"})
	for _, project := range f.Projects {
		add(sessioncatalog.DirectoryTarget{Path: desktopSessionDir(project.Root), Scope: "project", WorkspaceRoot: project.Root})
	}
	if a != nil {
		a.mu.RLock()
		collectTab := func(tab *WorkspaceTab) {
			if tab == nil {
				return
			}
			path := strings.TrimSpace(tab.currentSessionPath())
			if path == "" {
				return
			}
			add(sessioncatalog.DirectoryTarget{
				Path: filepath.Dir(path), Scope: tab.Scope, WorkspaceRoot: tab.WorkspaceRoot,
			})
		}
		for _, tab := range a.tabs {
			collectTab(tab)
		}
		for _, tab := range a.detachedSessions {
			collectTab(tab)
		}
		a.mu.RUnlock()
	}
	return sessioncatalog.UniqueDirectoryTargets(out)
}

func (a *App) indexRestoredSessionPaths(ctx context.Context, catalog *sessioncatalog.Catalog) {
	type restored struct {
		target sessioncatalog.DirectoryTarget
		path   string
	}
	a.mu.RLock()
	items := make([]restored, 0, len(a.tabs)+len(a.detachedSessions))
	collect := func(tab *WorkspaceTab) {
		if tab == nil {
			return
		}
		path := strings.TrimSpace(tab.currentSessionPath())
		if path == "" {
			return
		}
		items = append(items, restored{
			target: sessioncatalog.DirectoryTarget{Path: filepath.Dir(path), Scope: tab.Scope, WorkspaceRoot: tab.WorkspaceRoot},
			path:   path,
		})
	}
	for _, tab := range a.tabs {
		collect(tab)
	}
	for _, tab := range a.detachedSessions {
		collect(tab)
	}
	a.mu.RUnlock()
	for _, item := range items {
		if ctx.Err() != nil {
			return
		}
		_ = catalog.IndexSessionPath(ctx, item.target, item.path)
	}
}

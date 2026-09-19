package main

import (
	"context"
	"sort"
	"strings"

	"reasonix/desktop/internal/workspacestate"
	"reasonix/internal/config"
	"reasonix/internal/session"
)

func sessionRoute(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	return remoteSessionIDRoutePrefix + id
}

func parseSessionRoute(route string) (string, bool) {
	locator := classifySessionLocator(route)
	if locator.kind != sessionLocatorCanonical {
		return "", false
	}
	return locator.ref.SessionID, true
}

func (a *App) listCanonicalSessionsFromDir(dir, active string) []SessionMeta {
	service := a.desktopSessionService(dir)
	if service == nil {
		return []SessionMeta{}
	}
	activeID, _ := parseSessionRoute(active)
	state, err := a.workspaceRegistry().Load(a.bootContext())
	if err != nil {
		return []SessionMeta{{Error: "workspace_registry_unavailable", TurnsState: "corrupt"}}
	}
	allowed := map[string]bool{}
	for _, workspace := range state.Workspaces {
		matches := sameDesktopPath(dir, desktopSessionDir(workspace.Root))
		if workspace.ID == workspacestate.GlobalWorkspaceID {
			matches = matches || sameDesktopPath(dir, config.SessionDir())
		}
		if matches {
			for _, id := range workspace.SessionIDs {
				if state.SessionStates[id].Lifecycle == workspacestate.Active {
					allowed[id] = true
				}
			}
		}
	}
	query := service.Query()
	result := make([]SessionMeta, 0)
	cursor := ""
	for {
		page, err := query.List(context.Background(), cursor, 100)
		if err != nil {
			return result
		}
		for _, info := range page.Sessions {
			if !allowed[info.SessionID] {
				continue
			}
			delete(allowed, info.SessionID)
			meta := SessionMeta{
				Path: sessionRoute(info.SessionID), SessionID: info.SessionID,
				HostID: service.HostID(), Codec: info.Codec, Error: info.Error,
				Title: info.Title, Turns: info.Turns, TurnsState: "valid",
				CreatedAt: info.CreatedAt.UnixMilli(), LastActivityAt: info.UpdatedAt.UnixMilli(),
				ModTime: info.UpdatedAt.UnixMilli(), Current: info.SessionID == activeID,
			}
			if info.Error != "" {
				meta.TurnsState = "corrupt"
			} else {
				meta.Preview = info.Preview
			}
			a.mu.RLock()
			_, meta.Open = a.runtimeBySessionKey[sessionRuntimeKey(meta.Path)]
			a.mu.RUnlock()
			result = append(result, meta)
		}
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	for id := range allowed {
		result = append(result, SessionMeta{Path: sessionRoute(id), SessionID: id, HostID: localDesktopHostID, Error: "session_content_unavailable", TurnsState: "corrupt"})
	}
	sort.SliceStable(result, func(i, j int) bool { return result[i].LastActivityAt > result[j].LastActivityAt })
	return result
}

func sessionRefForRoute(service *session.Service, route string) (session.SessionRef, bool) {
	id, ok := parseSessionRoute(route)
	if !ok || service == nil {
		return session.SessionRef{}, false
	}
	return session.SessionRef{HostID: service.HostID(), SessionID: id}, true
}

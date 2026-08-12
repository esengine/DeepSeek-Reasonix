package main

import (
	"fmt"
	"strings"

	"reasonix/internal/agent"
	"reasonix/internal/control"
	"reasonix/internal/taskcatalog"
	"reasonix/internal/taskmonitor"
)

type TaskPageRequest struct {
	Scope      string   `json:"scope"`
	TabID      string   `json:"tabId"`
	ProjectKey string   `json:"projectKey"`
	States     []string `json:"states"`
	Query      string   `json:"query"`
	Cursor     string   `json:"cursor"`
	Limit      int      `json:"limit"`
}

type TaskCatalogItem = taskcatalog.Item
type TaskCatalogStatus = taskcatalog.Status

type TaskPage struct {
	Items       []TaskCatalogItem `json:"items"`
	NextCursor  string            `json:"nextCursor"`
	Revision    uint64            `json:"revision"`
	Partial     bool              `json:"partial"`
	StaleCursor bool              `json:"staleCursor"`
	Status      TaskCatalogStatus `json:"status"`
}

type TaskEventPageRequest struct {
	ProjectKey string `json:"projectKey"`
	TaskID     string `json:"taskId"`
	After      int    `json:"after"`
	Limit      int    `json:"limit"`
}

type TaskEventPage = taskcatalog.EventPage

type TaskActionRequest struct {
	ProjectKey      string `json:"projectKey"`
	TaskID          string `json:"taskId"`
	ExpectedVersion uint64 `json:"expectedVersion"`
	Reason          string `json:"reason"`
	IdempotencyKey  string `json:"idempotencyKey"`
}

type TaskOpenRequest struct {
	ProjectKey string `json:"projectKey"`
	TaskID     string `json:"taskId"`
}

func (a *App) GetTaskCatalogStatus() TaskCatalogStatus {
	if catalog := taskcatalog.Shared(); catalog != nil {
		return catalog.Status()
	}
	return TaskCatalogStatus{State: "opening", Mode: "memory", Pending: 1}
}

func (a *App) taskProjectKeys(req TaskPageRequest) ([]string, string, error) {
	switch strings.TrimSpace(req.Scope) {
	case "session":
		target, err := a.taskMonitorTargetForTab(req.TabID)
		if err != nil {
			return nil, "", err
		}
		key := taskcatalog.RegisterSharedProject(target.projectDir, workspaceName(target.projectDir))
		return []string{key}, target.sessionID, nil
	case "project", "":
		key := strings.TrimSpace(req.ProjectKey)
		if key == "" {
			root := a.projectDir()
			key = taskcatalog.RegisterSharedProject(root, workspaceName(root))
		} else if !a.allowedTaskProjectKey(key) {
			return nil, "", fmt.Errorf("unknown project key")
		}
		return []string{key}, "", nil
	case "all":
		projects := loadProjectsFile()
		keys := []string{taskcatalog.RegisterSharedProject(globalWorkspaceRoot(), projects.GlobalTitle)}
		for _, project := range projects.Projects {
			keys = append(keys, taskcatalog.RegisterSharedProject(project.Root, projectDisplayName(project)))
		}
		return keys, "", nil
	default:
		return nil, "", fmt.Errorf("unknown task scope %q", req.Scope)
	}
}

func (a *App) allowedTaskProjectRoots() []string {
	roots := []string{globalWorkspaceRoot(), a.projectDir()}
	for _, project := range loadProjectsFile().Projects {
		roots = append(roots, project.Root)
	}
	return roots
}

func (a *App) allowedTaskProjectKey(key string) bool {
	_, ok := a.resolveTaskProject(key)
	return ok
}

// resolveTaskProject maps a project key to an allowlisted workspace root without
// consulting the SQLite task catalog. Control actions use FileStore as authority.
func (a *App) resolveTaskProject(key string) (taskcatalog.Project, bool) {
	key = strings.TrimSpace(key)
	if key == "" {
		return taskcatalog.Project{}, false
	}
	projects := loadProjectsFile()
	labels := map[string]string{
		globalWorkspaceRoot(): projects.GlobalTitle,
		a.projectDir():        workspaceName(a.projectDir()),
	}
	for _, project := range projects.Projects {
		labels[project.Root] = projectDisplayName(project)
	}
	for _, root := range a.allowedTaskProjectRoots() {
		if taskcatalog.ProjectKey(root) != key {
			continue
		}
		label := labels[root]
		if strings.TrimSpace(label) == "" {
			label = workspaceName(root)
		}
		return taskcatalog.Project{Key: key, Root: root, Label: label}, true
	}
	return taskcatalog.Project{}, false
}

func (a *App) ListTaskPage(req TaskPageRequest) TaskPage {
	status := a.GetTaskCatalogStatus()
	out := TaskPage{Items: []TaskCatalogItem{}, Status: status, Partial: true}
	catalog := taskcatalog.Shared()
	if catalog == nil {
		return out
	}
	keys, sessionID, err := a.taskProjectKeys(req)
	if err != nil {
		out.Status.LastError = err.Error()
		return out
	}
	for _, key := range keys {
		if _, ok, lookupErr := catalog.Project(a.bootContext(), key); lookupErr != nil || !ok {
			out.Status.LastError = "unknown project key"
			return out
		}
	}
	page, err := catalog.ListPage(a.bootContext(), taskcatalog.PageRequest{ProjectKeys: keys, SessionID: sessionID,
		States: req.States, Query: req.Query, Cursor: req.Cursor, Limit: req.Limit})
	if err != nil {
		out.Status.LastError = err.Error()
		return out
	}
	a.overlayTaskCatalogRuntime(page.Items)
	return TaskPage{Items: page.Items, NextCursor: page.NextCursor, Revision: page.Revision,
		Partial: page.Partial, StaleCursor: page.StaleCursor, Status: page.Status}
}

func (a *App) overlayTaskCatalogRuntime(items []TaskCatalogItem) {
	type controllerRef struct {
		projectKey string
		sessionID  string
		ctrl       control.SessionAPI
	}
	a.mu.RLock()
	tabs := a.runtimeTabsLocked()
	controllers := make([]controllerRef, 0, len(tabs))
	for _, tab := range tabs {
		if tab == nil || tab.Ctrl == nil {
			continue
		}
		root := strings.TrimSpace(tab.WorkspaceRoot)
		if root == "" {
			root = globalWorkspaceRoot()
		}
		controllers = append(controllers, controllerRef{projectKey: taskcatalog.ProjectKey(root), ctrl: tab.Ctrl})
	}
	a.mu.RUnlock()
	for i := range controllers {
		controllers[i].sessionID = agent.BranchID(controllers[i].ctrl.SessionPath())
	}

	type runtimeValue struct{ status string }
	running := map[string]runtimeValue{}
	for _, controller := range controllers {
		for _, job := range controller.ctrl.Jobs() {
			key := controller.projectKey + "\x00" + controller.sessionID + "\x00" + job.ID
			running[key] = runtimeValue{status: job.Status}
		}
	}
	for i := range items {
		key := items[i].ProjectKey + "\x00" + items[i].Task.SessionID + "\x00" + items[i].Task.JobID
		value, ok := running[key]
		if !ok {
			continue
		}
		items[i].Task.RuntimeState = taskmonitor.RuntimeStateAlive
		switch value.status {
		case "running":
			items[i].Task.State = taskmonitor.TaskStateRunning
		case "done":
			items[i].Task.RuntimeState = taskmonitor.RuntimeStateExited
			items[i].Task.State = taskmonitor.TaskStateSucceeded
		case "failed":
			items[i].Task.RuntimeState = taskmonitor.RuntimeStateExited
			items[i].Task.State = taskmonitor.TaskStateFailed
		case "killed", "interrupted":
			items[i].Task.RuntimeState = taskmonitor.RuntimeStateExited
			items[i].Task.State = taskmonitor.TaskStateCancelled
		}
	}
}

func (a *App) ListTaskEventPage(req TaskEventPageRequest) TaskEventPage {
	out := TaskEventPage{Items: []taskmonitor.TaskEvent{}, NextSequence: req.After}
	if !a.allowedTaskProjectKey(req.ProjectKey) {
		out.Partial = true
		return out
	}
	catalog := taskcatalog.Shared()
	if catalog == nil {
		out.Partial = true
		return out
	}
	page, err := catalog.ListEventPage(a.bootContext(), req.ProjectKey, req.TaskID, req.After, req.Limit)
	if err != nil {
		out.Partial = true
		return out
	}
	return page
}

func (a *App) taskActionProject(key string) (taskcatalog.Project, error) {
	project, ok := a.resolveTaskProject(key)
	if !ok {
		return taskcatalog.Project{}, fmt.Errorf("unknown project key")
	}
	// Catalog may supply a display label only. Never override the allowlisted
	// root — a stale or damaged SQLite projection must not redirect FileStore.
	if catalog := taskcatalog.Shared(); catalog != nil {
		if row, found, err := catalog.Project(a.bootContext(), project.Key); err == nil && found {
			if strings.TrimSpace(row.Label) != "" {
				project.Label = row.Label
			}
		}
	}
	return project, nil
}

func (a *App) StopTaskByKey(req TaskActionRequest) (taskmonitor.ControlResult, error) {
	project, err := a.taskActionProject(req.ProjectKey)
	if err != nil {
		return taskmonitor.ControlResult{}, err
	}
	return a.taskControl().StopTaskWithKiller(a.bootContext(), project.Root, req.TaskID, req.ExpectedVersion, req.Reason, req.IdempotencyKey,
		desktopTaskJobKiller{app: a, projectDir: project.Root})
}

func (a *App) CancelTaskByKey(req TaskActionRequest) (taskmonitor.ControlResult, error) {
	project, err := a.taskActionProject(req.ProjectKey)
	if err != nil {
		return taskmonitor.ControlResult{}, err
	}
	return a.taskControl().CancelTaskWithKiller(a.bootContext(), project.Root, req.TaskID, req.ExpectedVersion, req.Reason, req.IdempotencyKey,
		desktopTaskJobKiller{app: a, projectDir: project.Root})
}

func (a *App) RequeueTaskByKey(req TaskActionRequest) (taskmonitor.ControlResult, error) {
	project, err := a.taskActionProject(req.ProjectKey)
	if err != nil {
		return taskmonitor.ControlResult{}, err
	}
	return a.taskControl().RequeueTask(a.bootContext(), project.Root, req.TaskID, req.ExpectedVersion, req.IdempotencyKey)
}

func (a *App) OpenTaskSessionByKey(req TaskOpenRequest) (taskmonitor.ControlResult, error) {
	project, err := a.taskActionProject(req.ProjectKey)
	if err != nil {
		return taskmonitor.ControlResult{}, err
	}
	return a.taskControl().OpenTaskSession(a.bootContext(), project.Root, req.TaskID)
}

func (a *App) RebuildTaskCatalog() error {
	if a == nil || a.shuttingDown.Load() {
		return fmt.Errorf("application is shutting down")
	}
	projects := loadProjectsFile()
	items := []taskcatalog.Project{{Root: globalWorkspaceRoot(), Label: projects.GlobalTitle}}
	for _, project := range projects.Projects {
		items = append(items, taskcatalog.Project{Root: project.Root, Label: projectDisplayName(project)})
	}
	return taskcatalog.RebuildSharedCatalog(a.bootContext(), items)
}

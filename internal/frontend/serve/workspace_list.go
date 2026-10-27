package serve

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"slices"

	"reasonix/internal/contract/config"
	"reasonix/internal/state/workspacelist"
)

type workspaceList = workspacelist.List

var errWorkspaceNotRemembered = errors.New("workspace is not remembered")
var errWorkspaceListFull = errors.New("the project list is at the limit you set (desktop.max_workspaces); remove one before adding another")

// workspaceListFull says whether the user's own limit leaves no room for another
// project. No limit is set unless their file sets one.
func workspaceListFull(n int) bool {
	limit := config.LoadForEdit(config.UserConfigPath()).WorkspaceLimit()
	return limit > 0 && n >= limit
}

func workspacesPath() string {
	if dir := config.MemoryUserDir(); dir != "" {
		return filepath.Join(dir, workspacelist.FileName)
	}
	return ""
}

// Workspaces is the sidebar's remembered project order.
func Workspaces() []string {
	list, _ := readWorkspaceList(workspacesPath())
	return list.Paths
}

// LaunchWorkspaces puts the launch project first without changing sidebar order.
func LaunchWorkspaces() []string {
	list, _ := readWorkspaceList(workspacesPath())
	if list.Launch == "" {
		return list.Paths
	}
	paths := []string{list.Launch}
	for _, dir := range list.Paths {
		if dir != list.Launch {
			paths = append(paths, dir)
		}
	}
	return paths
}

func readWorkspaceList(path string) (workspaceList, error) { return workspacelist.Read(path) }

func updateWorkspaceList(ctx context.Context, repair bool, mutate func(*workspaceList) error) error {
	return workspacelist.Update(ctx, workspacesPath(), repair, mutate)
}

// isAutoWorkspace says whether dir is the folder the app opens when nobody chose
// one. It is where such a launch lands, not a project, so no list holds it.
func isAutoWorkspace(dir string) bool {
	auto := config.AutoWorkspaceDir()
	return auto != "" && projectIdentity(dir) == projectIdentity(auto)
}

// ensureAutoWorkspace makes the app's own folder on demand and returns it, or ""
// when the state root will not take it. A new session with no project behind it
// opens there instead of being refused.
func ensureAutoWorkspace() string {
	auto := config.AutoWorkspaceDir()
	if auto == "" || os.MkdirAll(auto, 0o755) != nil {
		return ""
	}
	return auto
}

func rememberWorkspace(dir string) {
	if err := addRememberedWorkspace(context.Background(), dir); err != nil {
		slog.Warn("serve: remember workspace", "err", err)
	}
}

func addRememberedWorkspace(ctx context.Context, dir string) error {
	if dir == "" || isAutoWorkspace(dir) {
		return nil
	}
	return updateWorkspaceList(ctx, true, func(list *workspaceList) error {
		if slices.Contains(list.Paths, dir) {
			return nil
		}
		if workspaceListFull(len(rememberedProjects(list.Paths))) && !hasWorkspaceProject(list.Paths, dir) {
			return refusal(http.StatusConflict, "workspace.limit_reached", errWorkspaceListFull, nil)
		}
		identity := projectIdentity(dir)
		at := slices.IndexFunc(list.Paths, func(path string) bool { return projectIdentity(path) == identity })
		if at < 0 {
			list.Paths = append([]string{dir}, list.Paths...)
		} else {
			list.Paths = slices.Insert(list.Paths, at+1, dir)
		}
		list.Launch = dir
		return nil
	})
}

// keepUsedWorkspace lists a folder a conversation now lives in, behind the ones
// already there so neither the order nor the launch project moves. A pane
// adopted at launch is no folder anyone picked, and its conversations are
// reachable only through a row for it.
func keepUsedWorkspace(dir string) {
	if dir == "" || isAutoWorkspace(dir) {
		return
	}
	err := updateWorkspaceList(context.Background(), true, func(list *workspaceList) error {
		if slices.Contains(list.Paths, dir) || (workspaceListFull(len(rememberedProjects(list.Paths))) && !hasWorkspaceProject(list.Paths, dir)) {
			return nil
		}
		list.Paths = append(list.Paths, dir)
		return nil
	})
	if err != nil {
		slog.Warn("serve: list the folder a conversation lives in", "err", err)
	}
}

func forgetWorkspace(dir string) {
	if dir == "" {
		return
	}
	err := updateWorkspaceList(context.Background(), true, func(list *workspaceList) error {
		root := projectIdentity(dir)
		list.Paths = slices.DeleteFunc(list.Paths, func(path string) bool { return projectIdentity(path) == root })
		if projectIdentity(list.Launch) == root {
			list.Launch = ""
			if len(list.Paths) > 0 {
				list.Launch = list.Paths[0]
			}
		}
		return nil
	})
	if err != nil {
		slog.Warn("serve: forget workspace", "err", err)
	}
}

func moveWorkspace(ctx context.Context, dir string, direction int) error {
	return updateWorkspaceList(ctx, false, func(list *workspaceList) error {
		roots := workspaceProjectOrder(list.Paths)
		at := slices.Index(roots, projectIdentity(dir))
		if at < 0 {
			return errWorkspaceNotRemembered
		}
		to := at + direction
		if to >= 0 && to < len(roots) {
			roots[at], roots[to] = roots[to], roots[at]
			list.Paths = orderWorkspaceMembers(list.Paths, roots)
		}
		return nil
	})
}

// placeWorkspace drops dir where a drag left it: before the remembered project
// named by before, or at the end when before is empty. The launch project stays.
func placeWorkspace(ctx context.Context, dir, before string) error {
	return updateWorkspaceList(ctx, false, func(list *workspaceList) error {
		roots := workspaceProjectOrder(list.Paths)
		dir = projectIdentity(dir)
		if before != "" {
			before = projectIdentity(before)
		}
		if !slices.Contains(roots, dir) {
			return errWorkspaceNotRemembered
		}
		if before != "" && !slices.Contains(roots, before) {
			return errWorkspaceNotRemembered
		}
		if dir == before {
			return nil
		}
		rest := slices.DeleteFunc(roots, func(path string) bool { return path == dir })
		at := len(rest)
		if before != "" {
			at = slices.Index(rest, before)
		}
		list.Paths = orderWorkspaceMembers(list.Paths, slices.Insert(rest, at, dir))
		return nil
	})
}

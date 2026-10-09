package serve

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"path/filepath"
	"slices"

	"reasonix/internal/contract/config"
	"reasonix/internal/state/workspacelist"
)

type workspaceList = workspacelist.List

var errWorkspaceNotRemembered = errors.New("workspace is not remembered")
var errWorkspaceListFull = errors.New("the project list already contains 32 projects; remove one before adding another")

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

func rememberWorkspace(dir string) {
	if err := addRememberedWorkspace(context.Background(), dir); err != nil {
		slog.Warn("serve: remember workspace", "err", err)
	}
}

func addRememberedWorkspace(ctx context.Context, dir string) error {
	if dir == "" {
		return nil
	}
	return updateWorkspaceList(ctx, true, func(list *workspaceList) error {
		if slices.Contains(list.Paths, dir) {
			return nil
		}
		if len(list.Paths) >= workspaceRecentMax {
			return refusal(http.StatusConflict, "workspace.limit_reached", errWorkspaceListFull, nil)
		}
		list.Paths = append([]string{dir}, list.Paths...)
		list.Launch = dir
		return nil
	})
}

// keepUsedWorkspace lists a folder a conversation now lives in, behind the ones
// already there so neither the order nor the launch project moves. A pane
// adopted at launch is no folder anyone picked, and its conversations are
// reachable only through a row for it.
func keepUsedWorkspace(dir string) {
	if dir == "" {
		return
	}
	err := updateWorkspaceList(context.Background(), true, func(list *workspaceList) error {
		if slices.Contains(list.Paths, dir) || len(list.Paths) >= workspaceRecentMax {
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
		list.Paths = slices.DeleteFunc(list.Paths, func(path string) bool { return path == dir })
		if list.Launch == dir {
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
		at := slices.Index(list.Paths, dir)
		if at < 0 {
			return errWorkspaceNotRemembered
		}
		to := at + direction
		if to >= 0 && to < len(list.Paths) {
			list.Paths[at], list.Paths[to] = list.Paths[to], list.Paths[at]
		}
		return nil
	})
}

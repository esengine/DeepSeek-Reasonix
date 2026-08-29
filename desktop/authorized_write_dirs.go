package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/sandbox"
)

// Authorized write-directory management (desktop surface for #9167).
//
// These back the "authorized write directories" panel: querying what a project
// and the active session are currently allowed to write, and adding/removing
// entries. Project-scope writes persist into the project config
// [sandbox].allow_write (via SetProjectWriteAccess); session-scope writes
// apply to the active controller's in-memory session roots.

// WriteDirScope mirrors sandbox.ApprovalScope over the IPC integer boundary.
// Frontend sends 0 for project-level, 1 for session-level.
const (
	writeDirScopeProject = 0
	writeDirScopeSession = 1
)

// AuthorizedWriteDirs is the Wails-facing result for the write-directory
// management panel. It is a single struct (not multiple return values) because
// Wails binds only one value (or value+error); returning three scalars would be
// dropped and the frontend would receive nil.
type AuthorizedWriteDirs struct {
	Project []string `json:"project"`
	Global  []string `json:"global"`
	Session []string `json:"session"`
}

// QueryAuthorizedWriteDirs returns the currently authorized write directories
// for the active workspace: project (config allow_write), global (user-global
// common dirs), and session (active controller's session roots).
func (a *App) QueryAuthorizedWriteDirs() *AuthorizedWriteDirs {
	res := &AuthorizedWriteDirs{Project: []string{}, Global: []string{}, Session: []string{}}
	if a == nil {
		return res
	}
	// Global common dirs are user-level: always read from the user config,
	// independent of the active workspace root or controller (a controller
	// whose workspaceRoot is empty would otherwise hide them).
	if uc, err := config.LoadUserConfigReadOnly(); err == nil && uc != nil {
		res.Global = nonNilSlice(uc.GlobalAllowRoots())
	}
	_, ctrl := a.activeTabAndCtrl()
	if c, ok := ctrl.(*control.Controller); ok {
		p, _, s := c.QueryAuthorizedWriteDirs()
		res.Project = nonNilSlice(p)
		res.Session = nonNilSlice(s)
		return res
	}
	// No live controller: fall back to reading the project config allow_write
	// plus the user-global common dirs.
	if root := a.activeWorkspaceRoot(); root != "" {
		if cfg, err := config.LoadForRootReadOnly(root); err == nil && cfg != nil {
			res.Project = nonNilSlice(cfg.AllowWriteRoots())
			res.Global = nonNilSlice(appendUniq(res.Global, cfg.GlobalAllowRoots()))
		}
	}
	return res
}

// AddAuthorizedWriteDir adds a writable directory at the given scope
// (0=project, 1=session) for the active workspace/session.
func (a *App) AddAuthorizedWriteDir(scope int, dir string) error {
	if a == nil {
		return fmt.Errorf("no active app")
	}
	if strings.TrimSpace(dir) == "" {
		return fmt.Errorf("directory is required")
	}
	_, ctrl := a.activeTabAndCtrl()
	if c, ok := ctrl.(*control.Controller); ok {
		s := approvalScopeFromWriteDir(scope)
		return c.AddAuthorizedWriteDir(s, dir)
	}
	// No controller: project-only fallback using the active workspace root.
	if scope != writeDirScopeProject {
		return fmt.Errorf("no active session for session-scope write directory")
	}
	root := a.activeWorkspaceRoot()
	if root == "" {
		return fmt.Errorf("no active workspace")
	}
	path := projectConfigPathForWriteAccess(root)
	project := []string{dir}
	if cfg, err := config.LoadForRootReadOnly(root); err == nil && cfg != nil {
		project = nonNilSlice(cfg.AllowWriteRoots())
	}
	if !containsWriteDir(project, dir) {
		project = append(project, dir)
	}
	return config.SetProjectWriteAccess(path, project, "")
}

// RemoveAuthorizedWriteDir removes a writable directory at the given scope
// (0=project, 1=session) for the active workspace/session.
func (a *App) RemoveAuthorizedWriteDir(scope int, dir string) error {
	if a == nil {
		return fmt.Errorf("no active app")
	}
	if strings.TrimSpace(dir) == "" {
		return fmt.Errorf("directory is required")
	}
	_, ctrl := a.activeTabAndCtrl()
	if c, ok := ctrl.(*control.Controller); ok {
		s := approvalScopeFromWriteDir(scope)
		return c.RemoveAuthorizedWriteDir(s, dir)
	}
	if scope != writeDirScopeProject {
		return fmt.Errorf("no active session for session-scope write directory")
	}
	root := a.activeWorkspaceRoot()
	if root == "" {
		return fmt.Errorf("no active workspace")
	}
	path := projectConfigPathForWriteAccess(root)
	kept := []string{}
	if cfg, err := config.LoadForRootReadOnly(root); err == nil && cfg != nil {
		for _, p := range cfg.AllowWriteRoots() {
			if writeDirEqual(p, dir) {
				continue
			}
			kept = append(kept, p)
		}
	}
	return config.SetProjectWriteAccess(path, kept, "")
}

// AddGlobalWriteDir adds a user-global common directory (Settings → Permissions
// → global common dirs). It is honored across every project/session without
// approval, including subdirectories.
func (a *App) AddGlobalWriteDir(dir string) error {
	if a == nil {
		return fmt.Errorf("no active app")
	}
	if strings.TrimSpace(dir) == "" {
		return fmt.Errorf("directory is required")
	}
	_, ctrl := a.activeTabAndCtrl()
	if c, ok := ctrl.(*control.Controller); ok {
		return c.AddGlobalWriteDir(dir)
	}
	// No live controller: persist directly into the user config.
	cfg := []string{dir}
	if uc, err := config.LoadUserConfigReadOnly(); err == nil && uc != nil {
		cfg = nonNilSlice(uc.GlobalAllowRoots())
	}
	if !containsWriteDir(cfg, dir) {
		cfg = append(cfg, dir)
	}
	return config.SetGlobalWriteAccess(cfg)
}

// RemoveGlobalWriteDir removes a user-global common directory.
func (a *App) RemoveGlobalWriteDir(dir string) error {
	if a == nil {
		return fmt.Errorf("no active app")
	}
	if strings.TrimSpace(dir) == "" {
		return fmt.Errorf("directory is required")
	}
	_, ctrl := a.activeTabAndCtrl()
	if c, ok := ctrl.(*control.Controller); ok {
		return c.RemoveGlobalWriteDir(dir)
	}
	cfg := []string{}
	if uc, err := config.LoadUserConfigReadOnly(); err == nil && uc != nil {
		for _, g := range uc.GlobalAllowRoots() {
			if writeDirEqual(g, dir) {
				continue
			}
			cfg = append(cfg, g)
		}
	}
	return config.SetGlobalWriteAccess(cfg)
}

func approvalScopeFromWriteDir(scope int) sandbox.ApprovalScope {
	if scope == writeDirScopeSession {
		return sandbox.ApprovalScopeSession
	}
	return sandbox.ApprovalScopeProject
}

func nonNilSlice(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// appendUniq appends extra strings into roots, skipping duplicates.
func appendUniq(roots, extra []string) []string {
	for _, e := range extra {
		if e == "" || containsWriteDir(roots, e) {
			continue
		}
		roots = append(roots, e)
	}
	return roots
}

// projectConfigPathForWriteAccess matches control's helper: workspaceRoot/
// reasonix.toml when a workspace is active, else user config.
func projectConfigPathForWriteAccess(workspaceRoot string) string {
	if workspaceRoot != "" {
		return filepath.Join(workspaceRoot, "reasonix.toml")
	}
	path := config.SourcePath()
	if path == "" {
		path = "reasonix.toml"
	}
	return path
}

func containsWriteDir(roots []string, target string) bool {
	for _, r := range roots {
		if writeDirEqual(r, target) {
			return true
		}
	}
	return false
}

func writeDirEqual(a, b string) bool {
	if strings.TrimSpace(a) == "" || strings.TrimSpace(b) == "" {
		return a == b
	}
	absA, errA := filepath.Abs(a)
	absB, errB := filepath.Abs(b)
	if errA == nil && errB == nil {
		return absA == absB
	}
	return strings.TrimSpace(a) == strings.TrimSpace(b)
}

// PickGlobalWriteDir opens a directory picker so the user can choose a folder to
// add to the user-global common-directory list ([sandbox] allow_global) instead
// of typing the path by hand. It only returns the selected absolute path;
// AddGlobalWriteDir performs normalization and writes config.
func (a *App) PickGlobalWriteDir() (string, error) {
	if a == nil || a.ctx == nil {
		return "", nil
	}
	cur := a.activeWorkspaceRoot()
	if strings.TrimSpace(cur) == "" {
		cur, _ = os.Getwd()
	}
	dir, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title:            "Choose global write directory",
		DefaultDirectory: dialogDefaultDirectory(cur),
	})
	if err != nil || dir == "" {
		return "", err
	}
	return filepath.Clean(dir), nil
}

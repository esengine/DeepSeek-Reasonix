package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"reasonix/internal/config"
)

const (
	externalOpenerFileManager = "file-manager"
	externalOpenerEditor      = "editor"
	externalOpenerTerminal    = "terminal"
	externalOpenerCatalogTTL  = 15 * time.Second
)

// ExternalOpenerView is the renderer-safe description of one installed app.
// Target executable paths and launch arguments stay in the native shell so the
// frontend can never turn this feature into an arbitrary command runner.
type ExternalOpenerView struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	IconDataURL string `json:"iconDataUrl,omitempty"`
}

// ExternalOpenersView is the complete state for the Codex-style Open control.
type ExternalOpenersView struct {
	Openers           []ExternalOpenerView `json:"openers"`
	Preferred         string               `json:"preferred"`
	WorkspaceOpenable bool                 `json:"workspaceOpenable,omitempty"`
}

type externalOpenerSpec struct {
	View       ExternalOpenerView
	Target     string
	LaunchMode string
	IconSource string
}

type externalOpenerCatalogCache struct {
	mu          sync.Mutex
	ttl         time.Duration
	discover    func() []externalOpenerSpec
	now         func() time.Time
	loaded      bool
	loadedAt    time.Time
	specs       []externalOpenerSpec
	refreshDone chan struct{}
}

var platformExternalOpenerCatalog = newExternalOpenerCatalogCache(externalOpenerCatalogTTL, platformExternalOpenerSpecs)

func newExternalOpenerCatalogCache(ttl time.Duration, discover func() []externalOpenerSpec) *externalOpenerCatalogCache {
	return &externalOpenerCatalogCache{ttl: ttl, discover: discover, now: time.Now}
}

func cloneExternalOpenerSpecs(specs []externalOpenerSpec) []externalOpenerSpec {
	return append([]externalOpenerSpec(nil), specs...)
}

func (c *externalOpenerCatalogCache) get() []externalOpenerSpec {
	for {
		c.mu.Lock()
		now := c.now()
		if c.loaded && now.Sub(c.loadedAt) < c.ttl {
			specs := cloneExternalOpenerSpecs(c.specs)
			c.mu.Unlock()
			return specs
		}
		if done := c.refreshDone; done != nil {
			c.mu.Unlock()
			<-done
			continue
		}

		done := make(chan struct{})
		c.refreshDone = done
		discover := c.discover
		c.mu.Unlock()

		specs := discover()

		c.mu.Lock()
		c.specs = cloneExternalOpenerSpecs(specs)
		c.loaded = true
		c.loadedAt = c.now()
		c.refreshDone = nil
		close(done)
		result := cloneExternalOpenerSpecs(c.specs)
		c.mu.Unlock()
		return result
	}
}

func cachedPlatformExternalOpenerSpecs() []externalOpenerSpec {
	return platformExternalOpenerCatalog.get()
}

// startDetachedExternalOpener reaps the launched process in the background so
// exited children do not pile up as zombies for the desktop app's lifetime.
func startDetachedExternalOpener(cmd *exec.Cmd) error {
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

func externalOpenerByID(specs []externalOpenerSpec, id string) (externalOpenerSpec, bool) {
	id = strings.ToLower(strings.TrimSpace(id))
	for _, spec := range specs {
		if spec.View.ID == id {
			return spec, true
		}
	}
	return externalOpenerSpec{}, false
}

func resolveExternalOpener(specs []externalOpenerSpec, preferred string) (externalOpenerSpec, bool) {
	if spec, ok := externalOpenerByID(specs, preferred); ok {
		return spec, true
	}
	for _, spec := range specs {
		if spec.View.Kind == externalOpenerFileManager {
			return spec, true
		}
	}
	if len(specs) == 0 {
		return externalOpenerSpec{}, false
	}
	return specs[0], true
}

func externalOpenerViews(specs []externalOpenerSpec) []ExternalOpenerView {
	views := make([]ExternalOpenerView, 0, len(specs))
	seen := make(map[string]bool, len(specs))
	for _, spec := range specs {
		id := strings.ToLower(strings.TrimSpace(spec.View.ID))
		if id == "" || seen[id] || strings.TrimSpace(spec.View.Name) == "" {
			continue
		}
		spec.View.ID = id
		views = append(views, spec.View)
		seen[id] = true
	}
	return views
}

func externalOpenerViewsWithIcons(specs []externalOpenerSpec) []ExternalOpenerView {
	withIcons := make([]externalOpenerSpec, len(specs))
	copy(withIcons, specs)
	for i := range withIcons {
		withIcons[i].View.IconDataURL = externalOpenerIconDataURL(withIcons[i])
	}
	return externalOpenerViews(withIcons)
}

func (a *App) preferredExternalOpenerID() string {
	cfg, _, err := a.loadDesktopUserConfigForView()
	if err != nil || cfg == nil {
		return ""
	}
	return cfg.DesktopExternalOpener()
}

// ExternalOpeners returns only applications detected on the current machine.
// If a preference was copied from another OS or the app was uninstalled, the
// returned preferred id falls back without rewriting the user's config.
func (a *App) ExternalOpeners() ExternalOpenersView {
	specs := cachedPlatformExternalOpenerSpecs()
	views := externalOpenerViewsWithIcons(specs)
	selected, ok := resolveExternalOpener(specs, a.preferredExternalOpenerID())
	if !ok {
		return ExternalOpenersView{Openers: views}
	}
	return ExternalOpenersView{Openers: views, Preferred: selected.View.ID}
}

// ExternalOpenersForTab adds the tab-scoped local-workspace capability used by
// the chat-header Open control. Scope is intentionally not part of the check:
// both project tabs and Global tabs can own a real local workspace directory.
func (a *App) ExternalOpenersForTab(tabID string) ExternalOpenersView {
	if _, err := a.externalOpenerWorkspacePathForTab(tabID); err != nil {
		return ExternalOpenersView{Openers: []ExternalOpenerView{}}
	}
	view := a.ExternalOpeners()
	view.WorkspaceOpenable = true
	return view
}

// SetPreferredExternalOpener persists an installed, platform-owned opener id.
func (a *App) SetPreferredExternalOpener(id string) error {
	specs := cachedPlatformExternalOpenerSpecs()
	spec, ok := externalOpenerByID(specs, id)
	if !ok {
		return fmt.Errorf("external opener %q is not available", strings.TrimSpace(id))
	}
	return a.applyConfigOnly(func(c *config.Config) error {
		return c.SetDesktopExternalOpener(spec.View.ID)
	})
}

// OpenWorkspaceInExternalOpener opens the active workspace using either the
// requested installed app or the persisted/fallback selection when id is empty.
func (a *App) OpenWorkspaceInExternalOpener(id string) error {
	return a.OpenWorkspaceInExternalOpenerForTab("", id)
}

// OpenWorkspaceInExternalOpenerForTab is tab-scoped so a rapid tab switch cannot
// send the wrong project to an external application.
func (a *App) OpenWorkspaceInExternalOpenerForTab(tabID, id string) error {
	path, err := a.externalOpenerWorkspacePathForTab(tabID)
	if err != nil {
		return err
	}

	specs := cachedPlatformExternalOpenerSpecs()
	var spec externalOpenerSpec
	var ok bool
	if strings.TrimSpace(id) == "" {
		spec, ok = resolveExternalOpener(specs, a.preferredExternalOpenerID())
	} else {
		spec, ok = externalOpenerByID(specs, id)
	}
	if !ok {
		return fmt.Errorf("external opener %q is not available", strings.TrimSpace(id))
	}
	return launchPlatformExternalOpener(spec, path, true)
}

func (a *App) externalOpenerWorkspacePathForTab(tabID string) (string, error) {
	root, _, ok := a.workspaceTargetForTab(tabID)
	if !ok {
		return "", os.ErrNotExist
	}
	// A bound tab with no root has no stable workspace to expose. Keep the legacy
	// no-tab current-directory fallback, but do not turn an incomplete tab into
	// an opener for the Desktop process's launch directory.
	if strings.TrimSpace(root) == "" {
		return "", os.ErrNotExist
	}
	path, err := workspaceBaseFromRoot(root)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("workspace is not a directory")
	}
	return path, nil
}

// OpenLocalPathInExternalOpener opens an absolute local path with one of the
// detected, platform-owned applications. It shares OpenLocalPath's safety
// checks so a markdown link cannot turn an AI-generated executable path into
// an executable launch.
func (a *App) OpenLocalPathInExternalOpener(path, id string) error {
	path, err := normalizeLocalOpenPath(path)
	if err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !openTargetPathAllowed(path, info) {
		return fmt.Errorf("refusing to open executable target %q", path)
	}

	specs := cachedPlatformExternalOpenerSpecs()
	var spec externalOpenerSpec
	var ok bool
	if strings.TrimSpace(id) == "" {
		spec, ok = resolveExternalOpener(specs, a.preferredExternalOpenerID())
	} else {
		spec, ok = externalOpenerByID(specs, id)
	}
	if !ok {
		return fmt.Errorf("external opener %q is not available", strings.TrimSpace(id))
	}
	return launchPlatformExternalOpener(spec, path, info.IsDir())
}

// SaveLocalPathAs copies a local file to a user-selected destination without
// changing the source. Directories are intentionally excluded: Finder's
// reveal action is the appropriate operation for them.
func (a *App) SaveLocalPathAs(path string) (string, error) {
	path, err := normalizeLocalOpenPath(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("cannot save a directory as a file")
	}
	if a.ctx == nil {
		return "", nil
	}
	target, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:                "Save file as",
		DefaultDirectory:     filepath.Dir(path),
		DefaultFilename:      filepath.Base(path),
		CanCreateDirectories: true,
	})
	if err != nil || target == "" {
		return "", err
	}
	if filepath.Clean(target) == filepath.Clean(path) {
		return "", fmt.Errorf("destination is the same as the source")
	}
	if err := copyLocalPathAs(path, target); err != nil {
		return "", err
	}
	return target, nil
}

func copyLocalPathAs(path, target string) (err error) {
	src, err := os.Open(path)
	if err != nil {
		return err
	}
	defer src.Close()
	sourceInfo, err := src.Stat()
	if err != nil {
		return err
	}
	if sourceInfo.IsDir() {
		return fmt.Errorf("cannot save a directory as a file")
	}
	sameFile, err := localSaveDestinationIsSource(sourceInfo, target)
	if err != nil {
		return err
	}
	if sameFile {
		return fmt.Errorf("destination is the same as the source")
	}

	tmp, err := os.CreateTemp(filepath.Dir(target), "."+filepath.Base(target)+".reasonix-copy-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		if tmpName != "" {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err = io.Copy(tmp, src); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Chmod(sourceInfo.Mode().Perm()); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if err = replaceLocalSaveDestination(tmpName, target); err != nil {
		return err
	}
	tmpName = ""
	return nil
}

// localSaveDestinationIsSource compares filesystem identity, not just path
// spelling. os.Stat follows aliases, so this catches case-insensitive paths,
// symlinks, and hard links before the destination is replaced.
func localSaveDestinationIsSource(sourceInfo os.FileInfo, target string) (bool, error) {
	targetInfo, err := os.Stat(target)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return os.SameFile(sourceInfo, targetInfo), nil
}

// externalOpenerWorkingDirectory returns a valid directory for process launch.
// Editors still receive the original file path as an argument; only the
// process CWD changes when the requested path is a regular file.
func externalOpenerWorkingDirectory(path string) string {
	info, err := os.Stat(path)
	if err == nil && !info.IsDir() {
		return filepath.Dir(path)
	}
	return path
}

func externalOpenerLaunchPath(spec externalOpenerSpec, path string) string {
	if spec.View.Kind == externalOpenerTerminal || isTerminalLaunchMode(spec.LaunchMode) {
		return externalOpenerWorkingDirectory(path)
	}
	return path
}

func isTerminalLaunchMode(mode string) bool {
	switch mode {
	case "ghostty", "gnome-terminal", "konsole", "kitty", "alacritty", "cwd", "windows-terminal", "console":
		return true
	default:
		return false
	}
}

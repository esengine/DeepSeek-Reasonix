package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

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
	Openers   []ExternalOpenerView `json:"openers"`
	Preferred string               `json:"preferred"`
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
	root, _, ok := a.workspaceTargetForTab(tabID)
	if !ok {
		return os.ErrNotExist
	}
	path, err := workspaceBaseFromRoot(root)
	if err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("workspace is not a directory")
	}

	specs := cachedPlatformExternalOpenerSpecs()
	var spec externalOpenerSpec
	if strings.TrimSpace(id) == "" {
		spec, ok = resolveExternalOpener(specs, a.preferredExternalOpenerID())
	} else {
		spec, ok = externalOpenerByID(specs, id)
	}
	if !ok {
		return fmt.Errorf("external opener %q is not available", strings.TrimSpace(id))
	}
	return launchPlatformExternalOpener(spec, path)
}

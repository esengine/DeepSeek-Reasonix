// App-level Browser Companion wiring: the Wails-bound surface for the built-in
// browser and the host hooks that keep chat deletion and app shutdown in sync
// with the companion process.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"reasonix/desktop/internal/browseripc"
)

// BrowserDisposition selects where a chat link opens.
type BrowserDisposition string

const (
	BrowserDispositionForeground BrowserDisposition = "foreground"
	BrowserDispositionBackground BrowserDisposition = "background"
)

// BrowserDataClearRequest describes which browsing data to clear.
type BrowserDataClearRequest struct {
	Scopes []string `json:"scopes"`
}

// BrowserSiteGrantsView is the renderer-safe list of agent site grants.
type BrowserSiteGrantsView struct {
	Grants []browseripc.SiteGrant `json:"grants"`
}

// BrowserSettingsView is the renderer-safe browser settings surface.
type BrowserSettingsView struct {
	DefaultOpenMode string `json:"defaultOpenMode"` // "builtin" | "system"
}

// BrowserSettingsPatch is the accepted update shape; only non-empty fields
// apply.
type BrowserSettingsPatch struct {
	DefaultOpenMode string `json:"defaultOpenMode,omitempty"`
}

// OpenBrowserURL opens a chat link in the built-in browser. tabID selects the
// chat whose tab group receives the tab; an empty tabID resolves to the
// currently active chat. disposition is foreground or background. The backend
// accepts only http(s) URLs — file:// links keep their existing local-open
// path and mailto goes to the system browser — so a compromised renderer can
// never turn a link into an arbitrary protocol launch.
func (a *App) OpenBrowserURL(tabID, url, disposition string) error {
	url = strings.TrimSpace(url)
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return fmt.Errorf("built-in browser only opens http(s) links")
	}
	ownerID, err := a.browserOwnerForTab(tabID)
	if err != nil {
		return err
	}
	disp := browseripc.Disposition(disposition)
	if disp != browseripc.DispositionForeground && disp != browseripc.DispositionBackground {
		return fmt.Errorf("invalid disposition %q", disposition)
	}
	ctx, cancel := a.browserCallContext()
	defer cancel()
	var res browseripc.TabInfo
	return a.browser.Call(ctx, ownerID, "tab.open", browseripc.TabOpenParams{
		OwnerID: ownerID, URL: url, Disposition: disp,
	}, &res)
}

// OpenBrowserWindow focuses the companion window for the chat's tab group.
func (a *App) OpenBrowserWindow(tabID string) error {
	ownerID, err := a.browserOwnerForTab(tabID)
	if err != nil {
		return err
	}
	ctx, cancel := a.browserCallContext()
	defer cancel()
	var res json.RawMessage
	return a.browser.Call(ctx, ownerID, "window.focus", browseripc.OwnerParams{OwnerID: ownerID}, &res)
}

// GetBrowserStatus reports the companion lifecycle state for the settings and
// recovery surfaces.
func (a *App) GetBrowserStatus() BrowserCoordinatorView {
	view := a.browser.Status()
	view.AgentBrowserToolEnabled = a.browserToolsEnabled.Load()
	return view
}

// GetBrowserSettings returns the built-in browser settings.
func (a *App) GetBrowserSettings() BrowserSettingsView {
	settings := loadBrowserSettings()
	view := BrowserSettingsView{DefaultOpenMode: settings.DefaultOpenMode}
	if view.DefaultOpenMode == "" {
		view.DefaultOpenMode = browserDefaultOpenModeBuiltin
	}
	return view
}

// UpdateBrowserSettings applies a patch to the built-in browser settings.
func (a *App) UpdateBrowserSettings(patch BrowserSettingsPatch) error {
	settings := loadBrowserSettings()
	mode := strings.TrimSpace(patch.DefaultOpenMode)
	if mode != "" {
		if mode != browserDefaultOpenModeBuiltin && mode != browserDefaultOpenModeSystem {
			return fmt.Errorf("invalid default open mode %q", mode)
		}
		settings.DefaultOpenMode = mode
	}
	return saveBrowserSettings(settings)
}

// ClearBrowserData clears the requested browsing data scopes in the companion's
// isolated Chromium profile. The host tab-state file is never touched: history,
// cookies, cache, and downloads live in the profile, not in chat state.
func (a *App) ClearBrowserData(request BrowserDataClearRequest) ([]string, error) {
	if len(request.Scopes) == 0 {
		return []string{}, fmt.Errorf("no scopes requested")
	}
	scopes := make([]browseripc.ClearScope, 0, len(request.Scopes))
	for _, s := range request.Scopes {
		switch browseripc.ClearScope(s) {
		case browseripc.ClearHistory, browseripc.ClearCookies, browseripc.ClearCache,
			browseripc.ClearDownloads, browseripc.ClearAll:
			scopes = append(scopes, browseripc.ClearScope(s))
		default:
			return []string{}, fmt.Errorf("invalid scope %q", s)
		}
	}
	ctx, cancel := a.browserCallContext()
	defer cancel()
	var res browseripc.DataClearResult
	if err := a.browser.Call(ctx, "", "data.clear", browseripc.DataClearParams{Scopes: scopes}, &res); err != nil {
		return []string{}, err
	}
	if res.Cleared == nil {
		res.Cleared = []string{}
	}
	return res.Cleared, nil
}

// ListBrowserSiteGrants returns the recorded agent site-admission grants. The
// list is empty (never null) when the companion is absent.
func (a *App) ListBrowserSiteGrants() (BrowserSiteGrantsView, error) {
	ctx, cancel := a.browserCallContext()
	defer cancel()
	var res browseripc.PermissionsListResult
	if err := a.browser.Call(ctx, "", "permissions.list", struct{}{}, &res); err != nil {
		return BrowserSiteGrantsView{Grants: []browseripc.SiteGrant{}}, err
	}
	if res.Grants == nil {
		res.Grants = []browseripc.SiteGrant{}
	}
	return BrowserSiteGrantsView{Grants: res.Grants}, nil
}

// RevokeBrowserSiteGrant removes one exact normalized origin from the agent
// grants.
func (a *App) RevokeBrowserSiteGrant(origin string) error {
	ctx, cancel := a.browserCallContext()
	defer cancel()
	var res json.RawMessage
	return a.browser.Call(ctx, "", "permissions.revoke", browseripc.PermissionsRevokeParams{Origin: origin}, &res)
}

// InstallOrRepairBrowserComponent downloads the current platform companion,
// verifies its minisign signature and SHA-256, then atomically activates it.
func (a *App) InstallOrRepairBrowserComponent() error {
	ctx, cancel := context.WithTimeout(a.reqCtx(), 10*time.Minute)
	defer cancel()
	return a.installOrRepairBrowserComponent(ctx)
}

// browserOwnerForTab resolves the ownerId for a chat link open. An explicit
// tabID must reference a live chat; empty resolves to the active chat. The
// model never reaches this path: agent browser calls carry an ownerId bound at
// controller build time.
func (a *App) browserOwnerForTab(tabID string) (string, error) {
	if a.browser == nil {
		return "", fmt.Errorf("built-in browser is unavailable")
	}
	tabID = strings.TrimSpace(tabID)
	if tabID != "" {
		a.mu.RLock()
		_, ok := a.tabs[tabID]
		a.mu.RUnlock()
		if !ok {
			return "", fmt.Errorf("chat %q is not open", tabID)
		}
		return tabID, nil
	}
	a.mu.RLock()
	active := a.activeTabID
	a.mu.RUnlock()
	if active == "" {
		return "", fmt.Errorf("no active chat")
	}
	return active, nil
}

// browserCallContext bounds one browser call by the protocol response budget.
func (a *App) browserCallContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), a.browser.opts.responseTimeout)
}

// handleBrowserEvent receives companion notifications. Phase 1 surfaces the
// status changes; richer events (tab.changed, navigation, downloads,
// permission requests, agent takeover) become frontend runtime events as the
// companion implements them.
func (a *App) handleBrowserEvent(ev browseripc.Event) {
	switch ev.Event.Name {
	case "tab.changed":
		var data browseripc.TabChangedEventData
		if err := decodeBrowserEventData(ev, &data); err != nil {
			return
		}
		a.emitBrowserEvent("browser.tabChanged", map[string]any{
			"tabId":      data.TabID,
			"url":        data.URL,
			"title":      data.Title,
			"active":     data.Active,
			"generation": data.Generation,
		})
	}
}

// handleBrowserCompanionCrash is the host-side crash hook. The companion is
// restarted on the next use (with backoff); the frontend learns about the
// current state through GetBrowserStatus.
func (a *App) handleBrowserCompanionCrash() {
	a.emitBrowserEvent("browser.statusChanged", map[string]any{"state": string(browserCrashed)})
}

// emitBrowserEvent forwards a browser event to the frontend through the same
// async emitter the chat runtime uses. Emitting with a plain context outside a
// running Wails app is a no-op via the test fallback.
func (a *App) emitBrowserEvent(name string, payload map[string]any) {
	a.runtimeEvents.Emit(a.ctx, name, payload)
}

func decodeBrowserEventData(ev browseripc.Event, out any) error {
	if len(ev.Event.Data) == 0 {
		return nil
	}
	return json.Unmarshal(ev.Event.Data, out)
}

// removeBrowserOwnerForSession drops the browser tab state of a chat whose
// session was permanently deleted. Shared login state in the Chromium profile
// is intentionally preserved.
func (a *App) removeBrowserOwnerForSession(sessionPath string) {
	if a.browser == nil {
		return
	}
	a.mu.Lock()
	var ownerID string
	for _, tab := range a.tabs {
		if tab.currentSessionPath() == sessionPath {
			ownerID = tab.ID
			break
		}
	}
	a.mu.Unlock()
	if ownerID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = a.browser.RemoveOwner(ctx, ownerID)
	if a.browserState != nil {
		a.browserState.flush()
	}
}

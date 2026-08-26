package main

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"
)

// hasRemoteTabSurface is called while App.mu protects the local registry.
// The repository lock order is local App.mu before remoteTabMu.
func (a *App) hasRemoteTabSurface() bool {
	a.remoteTabMu.Lock()
	defer a.remoteTabMu.Unlock()
	return len(a.remoteTabs) > 0
}

// reconcileTabStripOrder merges the preferred persisted order with every
// currently live local and remote tab id.
func reconcileTabStripOrder(preferred, localIDs, remoteIDs []string) []string {
	valid := make(map[string]bool, len(localIDs)+len(remoteIDs))
	for _, id := range localIDs {
		valid[id] = true
	}
	for _, id := range remoteIDs {
		valid[id] = true
	}
	seen := make(map[string]bool, len(valid))
	out := make([]string, 0, len(valid))
	appendID := func(id string) {
		if valid[id] && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	for _, id := range preferred {
		appendID(id)
	}
	for _, id := range localIDs {
		appendID(id)
	}
	for _, id := range remoteIDs {
		appendID(id)
	}
	return out
}

func (a *App) remoteTabMetas(localIDs []string) ([]TabMeta, string, []string) {
	a.remoteTabMu.Lock()
	defer a.remoteTabMu.Unlock()
	ids := a.orderedRemoteTabIDsLocked()
	metas := make([]TabMeta, 0, len(ids))
	for _, id := range ids {
		if tab := a.remoteTabs[id]; tab != nil {
			meta := remoteTabMetaLocked(tab)
			meta.Active = id == a.remoteTabLayout.activeID
			metas = append(metas, meta)
		}
	}
	a.remoteTabLayout.stripOrder = reconcileTabStripOrder(a.remoteTabLayout.stripOrder, localIDs, ids)
	return metas, a.remoteTabLayout.activeID, append([]string(nil), a.remoteTabLayout.stripOrder...)
}

// orderedRemoteTabIDsLocked returns the remote strip order with self-repair:
// registry keys missing from the order append in sorted order (mirrors
// orderedTabIDsLocked for the local side). Caller holds remoteTabMu.
func (a *App) orderedRemoteTabIDsLocked() []string {
	seen := make(map[string]bool, len(a.remoteTabLayout.order))
	out := make([]string, 0, len(a.remoteTabs))
	for _, id := range a.remoteTabLayout.order {
		if a.remoteTabs[id] != nil && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	var missing []string
	for id := range a.remoteTabs {
		if !seen[id] {
			missing = append(missing, id)
		}
	}
	sort.Strings(missing)
	return append(out, missing...)
}

// remoteTabsFileEntries snapshots the persisted remote tab section (entries
// plus strip order plus the active remote id). Called from the tab-file write
// path — lock order tabsSaveMu → remoteTabMu.
func (a *App) remoteTabsFileEntries(localIDs []string) ([]desktopRemoteTabEntry, []string, []string, string) {
	a.remoteTabMu.Lock()
	defer a.remoteTabMu.Unlock()
	ids := a.orderedRemoteTabIDsLocked()
	entries := make([]desktopRemoteTabEntry, 0, len(ids))
	for _, id := range ids {
		tab := a.remoteTabs[id]
		if tab == nil {
			continue
		}
		entries = append(entries, desktopRemoteTabEntry{
			ID:         tab.id,
			HostID:     tab.ref.HostID,
			Workspace:  tab.ref.Workspace,
			TopicTitle: tab.topicTitle,
			Model:      tab.model,
		})
	}
	order := append([]string(nil), ids...)
	if len(order) == 0 {
		order = nil
	}
	stripOrder := reconcileTabStripOrder(a.remoteTabLayout.stripOrder, localIDs, ids)
	if len(entries) == 0 {
		stripOrder = nil
	}
	a.remoteTabLayout.stripOrder = append([]string(nil), stripOrder...)
	return entries, order, stripOrder, a.remoteTabLayout.activeID
}

// CloseRemoteTab tears down one remote tab: the SSE pump stops and the
// registry entry goes away. The remote serve and the SSH connection stay
// untouched — other tabs on the same host keep running.
func (a *App) CloseRemoteTab(tabID string) error {
	protectLastSurface := a.singleSurfaceLayoutEnabled()
	if protectLastSurface {
		a.singleSurfaceMu.Lock()
		defer a.singleSurfaceMu.Unlock()
	}
	return a.closeRemoteTabRegistration(tabID, !protectLastSurface)
}

// removeRemoteTabsForHost drops surfaces whose connection identity was
// deleted. If that removes the final visible surface, create a local blank in
// the same single-surface transaction so workbench/creation layouts never
// retain an uncloseable orphan or become surface-less.
func (a *App) removeRemoteTabsForHost(hostID string) error {
	protectLastSurface := a.singleSurfaceLayoutEnabled()
	if protectLastSurface {
		a.singleSurfaceMu.Lock()
		defer a.singleSurfaceMu.Unlock()
	}

	a.remoteTabMu.Lock()
	ids := make([]string, 0, len(a.remoteTabs))
	for id, tab := range a.remoteTabs {
		if tab != nil && tab.ref.HostID == hostID {
			ids = append(ids, id)
		}
	}
	a.remoteTabMu.Unlock()
	if len(ids) == 0 {
		return nil
	}
	for _, id := range ids {
		if err := a.closeRemoteTabRegistration(id, true); err != nil {
			return err
		}
	}

	a.mu.RLock()
	localCount := len(a.tabs)
	a.mu.RUnlock()
	a.remoteTabMu.Lock()
	remoteCount := len(a.remoteTabs)
	a.remoteTabMu.Unlock()
	if localCount+remoteCount > 0 {
		return nil
	}
	_, err := a.ensureBlankTab("global", "")
	return err
}

// closeRemoteTabRegistration performs the registry mutation. Callers that
// already hold singleSurfaceMu use allowEmpty only to roll back a tab whose
// open transaction failed before it became a usable surface.
func (a *App) closeRemoteTabRegistration(tabID string, allowEmpty bool) error {
	if !allowEmpty {
		a.mu.RLock()
		localCount := len(a.tabs)
		a.remoteTabMu.Lock()
		if localCount == 0 && len(a.remoteTabs) == 1 && a.remoteTabs[tabID] != nil {
			a.remoteTabMu.Unlock()
			a.mu.RUnlock()
			return fmt.Errorf("cannot close the last tab")
		}
		a.mu.RUnlock()
	} else {
		a.remoteTabMu.Lock()
	}
	tab := a.remoteTabs[tabID]
	closingActive := a.remoteTabLayout.activeID == tabID
	nextLocalID := ""
	closingIndex := -1
	for i, id := range a.remoteTabLayout.stripOrder {
		if id == tabID {
			closingIndex = i
			break
		}
	}
	delete(a.remoteTabs, tabID)
	a.remoteTabLayout.order = removeRemoteTabOrderID(a.remoteTabLayout.order, tabID)
	if closingActive {
		a.remoteTabLayout.activeID = ""
		remaining := removeRemoteTabOrderID(append([]string(nil), a.remoteTabLayout.stripOrder...), tabID)
		if len(remaining) > 0 && closingIndex >= 0 {
			nextIndex := closingIndex
			if nextIndex >= len(remaining) {
				nextIndex = len(remaining) - 1
			}
			if nextID := remaining[nextIndex]; a.remoteTabs[nextID] != nil {
				a.remoteTabLayout.activeID = nextID
			} else {
				nextLocalID = nextID
			}
		}
	}
	var cancel context.CancelFunc
	if tab != nil {
		cancel = tab.cancel
	}
	a.remoteTabMu.Unlock()
	if cancel != nil {
		cancel()
	}
	if closingActive && nextLocalID != "" {
		a.mu.Lock()
		if a.tabs[nextLocalID] != nil {
			a.activeTabID = nextLocalID
		}
		a.mu.Unlock()
	}
	a.saveTabsFromRemote()
	return nil
}

// remoteTabsHostStatus reacts to SSH transitions for every open tab on the
// host: losing the tunnel suspends the pumps, a regained connection
// re-attaches each tab to the still-running remote serve, and a terminal
// failure parks the tabs in error.
func (a *App) remoteTabsHostStatus(hostID, state, errText string) {
	switch state {
	case "connecting", "reconnecting":
		a.suspendRemoteTabPumps(hostID, "reconnecting", "")
	case "connected":
		a.resumeRemoteTabs(hostID)
	case "stopped":
		a.suspendRemoteTabPumps(hostID, "error", errText)
	}
}

func (a *App) suspendRemoteTabPumps(hostID, state, errText string) {
	a.remoteTabMu.Lock()
	affected := make([]string, 0, 2)
	for _, tab := range a.remoteTabs {
		if tab.ref.HostID != hostID || tab.state == "disconnected" || (tab.state == "connecting" && tab.client == nil) {
			// A restored shell was never connected this run: host status
			// transitions must not flip it into a runtime state. The same is
			// true for a first bootstrap that is still waiting for that host.
			continue
		}
		tab.gen++
		if tab.cancel != nil {
			tab.cancel()
			tab.cancel = nil
		}
		tab.state = state
		tab.err = errText
		affected = append(affected, tab.id)
	}
	a.remoteTabMu.Unlock()
	for _, tabID := range affected {
		a.emitRemoteEvent(fmt.Sprintf("remote-tab:%s:state", tabID), RemoteTabStateView{State: state, Error: errText})
	}
}

// parkRemoteTabsForServer intentionally retires pumps for one managed Serve.
// Cancelling generations before StopServer prevents their EOF path from
// interpreting an explicit stop as an unexpected disconnect and restarting it.
func (a *App) parkRemoteTabsForServer(hostID, workspace, state, errText string) []string {
	a.remoteTabMu.Lock()
	affected := make([]string, 0, 2)
	for _, tab := range a.remoteTabs {
		if tab.ref.HostID != hostID || tab.ref.Workspace != workspace {
			continue
		}
		tab.gen++
		if tab.cancel != nil {
			tab.cancel()
		}
		tab.cancel = nil
		tab.client = nil
		tab.base = ""
		tab.token = ""
		tab.state = state
		tab.err = errText
		affected = append(affected, tab.id)
	}
	a.remoteTabMu.Unlock()
	for _, tabID := range affected {
		a.emitRemoteEvent(fmt.Sprintf("remote-tab:%s:state", tabID), RemoteTabStateView{State: state, Error: errText})
	}
	return affected
}

// resumeRemoteTabs re-attaches every suspended tab of a reconnected host.
// The remote serve kept running through the SSH drop, so re-attachment only
// rebuilds the tunnel client and the event pump; the serve still holds the
// active session, so no session re-entry is needed.
func (a *App) resumeRemoteTabs(hostID string) {
	a.remoteTabMu.Lock()
	tabIDs := make([]string, 0, 2)
	for id, tab := range a.remoteTabs {
		if tab.ref.HostID == hostID && tab.state == "reconnecting" {
			tabIDs = append(tabIDs, id)
		}
	}
	a.remoteTabMu.Unlock()
	for _, tabID := range tabIDs {
		a.goSafe("remoteTabReattach", func() { a.reattachRemoteTab(tabID) })
	}
}

const remoteTabReattachAttempts = 3

// reattachRemoteTab rebuilds one tab's serve client and pump after the host
// connection came back. Transient failures retry while the same tab remains
// reconnecting; exhaustion parks it in user-retryable serve_down.
func (a *App) reattachRemoteTab(tabID string) {
	for attempt := range remoteTabReattachAttempts {
		if a.reattachRemoteTabOnce(tabID) {
			return
		}
		if attempt+1 < remoteTabReattachAttempts {
			time.Sleep(time.Duration(attempt+1) * 150 * time.Millisecond)
		}
	}
	a.remoteTabMu.Lock()
	tab := a.remoteTabs[tabID]
	stillReconnecting := tab != nil && tab.state == "reconnecting"
	a.remoteTabMu.Unlock()
	if stillReconnecting {
		a.emitRemoteTabState(tabID, "serve_down", "Remote session reconnect failed. Retry to restart the server.")
	}
}

func (a *App) reattachRemoteTabOnce(tabID string) bool {
	a.remoteTabMu.Lock()
	tab := a.remoteTabs[tabID]
	if tab == nil || tab.state != "reconnecting" {
		a.remoteTabMu.Unlock()
		return true
	}
	a.remoteTabMu.Unlock()
	tab.sessionMu.Lock()
	defer tab.sessionMu.Unlock()

	a.remoteTabMu.Lock()
	if a.remoteTabs[tabID] != tab || tab.state != "reconnecting" {
		a.remoteTabMu.Unlock()
		return true
	}
	hostID, workspace := tab.ref.HostID, tab.ref.Workspace
	previousInstanceID := tab.session.instanceID
	sessionName := strings.TrimSpace(tab.session.name)
	resetSession := tab.session.reset
	a.remoteTabMu.Unlock()

	rt, err := a.remoteRT()
	if err != nil {
		return false
	}
	ctx := a.bootContext()
	if ctx == nil {
		ctx = context.Background()
	}
	view, token, err := rt.EnsureServer(ctx, hostID, workspace)
	if err != nil || view.State != "ready" || view.LocalURL == "" {
		// EnsureServer errors can include remote process output, including
		// provider credentials forwarded during bootstrap. Keep reconnect
		// diagnostics structural so secrets can never reach desktop logs.
		log.Printf("[remote] reattachRemoteTab: EnsureServer NOT-READY tab=%s state=%s localURL=%q", tabID, view.State, view.LocalURL)
		return false
	}
	callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	client, clientErr := newServeHTTPClient(view.LocalURL)
	if clientErr != nil {
		return false
	}
	if err := serveHandshake(callCtx, client, view.LocalURL, token); err != nil {
		log.Printf("[remote] reattachRemoteTab: handshake FAILED tab=%s base=%q err=%v", tabID, view.LocalURL, err)
		return false
	}
	relaunched := previousInstanceID != "" && view.InstanceID != "" && previousInstanceID != view.InstanceID
	if relaunched && !resetSession && sessionName == "" {
		// A replacement Serve starts on a blank controller. Publishing ready in
		// that state would silently detach the tab from its conversation, so fail
		// closed until the user explicitly chooses a session or New Topic.
		log.Printf("[remote] reattachRemoteTab: replacement serve lacks session identity tab=%s", tabID)
		return false
	}

	a.remoteTabMu.Lock()
	if cur := a.remoteTabs[tabID]; cur != tab || tab.state != "reconnecting" {
		a.remoteTabMu.Unlock()
		return true
	}
	tab.gen++
	if tab.cancel != nil {
		tab.cancel()
	}
	tab.client = client
	tab.base = view.LocalURL
	tab.token = token
	gen := tab.gen
	pumpCtx, cancelPump := context.WithCancel(ctx)
	tab.cancel = cancelPump
	a.remoteTabMu.Unlock()

	opened := make(chan error, 1)
	a.goSafe("remoteTabPump", func() { a.remoteTabPump(pumpCtx, tabID, gen, opened) })
	select {
	case err := <-opened:
		if err != nil {
			a.retireRemoteTabGeneration(tabID, gen)
			a.emitRemoteTabState(tabID, "reconnecting", "")
			return false
		}
	case <-callCtx.Done():
		a.retireRemoteTabGeneration(tabID, gen)
		a.emitRemoteTabState(tabID, "reconnecting", "")
		return false
	}
	if relaunched {
		opts := RemoteTabOpenOptions{NewSession: resetSession, SessionName: sessionName}
		if err := enterRemoteSession(callCtx, client, view.LocalURL, opts); err != nil {
			log.Printf("[remote] reattachRemoteTab: session re-entry FAILED tab=%s err=%v", tabID, err)
			a.retireRemoteTabGeneration(tabID, gen)
			a.emitRemoteTabState(tabID, "reconnecting", "")
			return false
		}
	}
	if !a.waitRemoteTabStreamStable(callCtx, tabID, gen) {
		return false
	}
	a.remoteTabMu.Lock()
	if current := a.remoteTabs[tabID]; current == tab && current.gen == gen {
		current.session.instanceID = view.InstanceID
	}
	a.remoteTabMu.Unlock()
	if !a.transitionRemoteTabState(tabID, gen, "reconnecting", "ready", "") {
		a.retireRemoteTabGeneration(tabID, gen)
		return false
	}
	return true
}

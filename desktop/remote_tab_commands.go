package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"reasonix/internal/config"
)

func (a *App) RenameRemoteProjectSession(hostID, workspace, name, title string) error {
	if err := setRemoteSessionTitleOverride(hostID, workspace, name, title); err != nil {
		return err
	}

	a.remoteTabMu.Lock()
	var live *remoteTab
	var liveID string
	var client *http.Client
	var base string
	for _, tab := range a.remoteTabs {
		if tab.ref.HostID == hostID && tab.ref.Workspace == workspace && tab.client != nil {
			live = tab
			liveID = tab.id
			client = tab.client
			base = tab.base
			break
		}
	}
	a.remoteTabMu.Unlock()
	if live == nil {
		return nil
	}
	if strings.TrimSpace(name) == "" {
		next := strings.TrimSpace(title)
		if next == "" {
			next = a.localizedDefaultTopicTitle()
		}
		a.remoteTabMu.Lock()
		current := a.remoteTabs[liveID]
		if current != live || current.client != client || !current.session.reset {
			a.remoteTabMu.Unlock()
			return nil
		}
		changed := current.topicTitle != next
		current.topicTitle = next
		meta := remoteTabMetaLocked(current)
		a.remoteTabMu.Unlock()
		if changed {
			a.emitRemoteEvent("remote-tab:updated", meta)
			a.saveTabsFromRemote()
		}
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	entries, err := serveSessions(ctx, client, base)
	if err != nil {
		return nil
	}
	for _, entry := range entries {
		if !entry.Current || entry.Name != name {
			continue
		}
		next := strings.TrimSpace(title)
		if next == "" {
			next = strings.TrimSpace(entry.Title)
		}
		if next == "" {
			next = remoteWorkspaceName(workspace)
		}
		a.remoteTabMu.Lock()
		current := a.remoteTabs[liveID]
		if current != live || current.client != client {
			a.remoteTabMu.Unlock()
			return nil
		}
		changed := current.topicTitle != next
		if changed {
			current.topicTitle = next
		}
		meta := remoteTabMetaLocked(current)
		a.remoteTabMu.Unlock()
		if changed {
			a.emitRemoteEvent("remote-tab:updated", meta)
			a.saveTabsFromRemote()
		}
		return nil
	}
	return nil
}

func (a *App) resumeRemoteTabSession(tabID, name string) {
	a.resumeRemoteTabSessionPath(tabID, name, "", "")
}

func (a *App) resumeRemoteTabSessionPath(tabID, name, sessionPath, sessionTitle string) {
	a.resumeRemoteTabSessionPathForSelection(tabID, name, sessionPath, sessionTitle, 0)
}

func (a *App) resumeRemoteTabSessionPathForSelection(tabID, name, sessionPath, sessionTitle string, selectionRevision uint64) bool {
	return a.resumeRemoteTabSessionPathForOpenSelection(tabID, name, sessionPath, sessionTitle, selectionRevision, nil)
}

func (a *App) resumeRemoteTabSessionPathForOpenSelection(tabID, name, sessionPath, sessionTitle string, selectionRevision uint64, previous *remoteTabOpenSelection) bool {
	a.remoteTabMu.Lock()
	tab := a.remoteTabs[tabID]
	a.remoteTabMu.Unlock()
	if tab == nil {
		return true
	}
	tab.sessionMu.Lock()
	defer tab.sessionMu.Unlock()
	a.remoteTabMu.Lock()
	if a.remoteTabs[tabID] != tab || (selectionRevision != 0 && tab.selectionRevision != selectionRevision) {
		a.remoteTabMu.Unlock()
		return true
	}
	if tab.client == nil || tab.state != "ready" {
		if selectionRevision != 0 && (tab.state == "connecting" || tab.state == "reconnecting") {
			requeueRemoteTabOpenSelectionLocked(tab, &remoteTabPendingOpenSelection{
				name: strings.TrimSpace(name), path: strings.TrimSpace(sessionPath), title: strings.TrimSpace(sessionTitle),
				revision: selectionRevision, deferred: true, identityCommitted: true, previous: previous,
			})
			a.remoteTabMu.Unlock()
			return true
		}
		a.remoteTabMu.Unlock()
		return false
	}
	consumeQueuedRemoteTabOpenSelectionLocked(tab, selectionRevision)
	client, base, gen := tab.client, tab.base, tab.gen
	a.remoteTabMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var target serveSessionEntry
	if sessionPath != "" {
		target = serveSessionEntry{Name: strings.TrimSpace(name), Path: strings.TrimSpace(sessionPath), Title: strings.TrimSpace(sessionTitle)}
	} else {
		entries, err := serveSessions(ctx, client, base)
		if err != nil {
			a.transitionRemoteTabState(tabID, gen, "ready", "ready", fmt.Sprintf("Could not open remote session %q: %v", name, err))
			return false
		}
		for _, entry := range entries {
			if entry.Name == name {
				target = entry
				break
			}
		}
	}
	if target.Path != "" {
		body, _ := json.Marshal(map[string]string{"path": target.Path})
		// /resume may reattach a controller already producing frames. Route them
		// before the request returns so the all-session pump does not discard its
		// handoff output or prompt replay as background work.
		route := a.beginRemoteTabProvisionalResume(tabID, tab, client, gen, target.Path)
		if err := servePost(ctx, client, serveURL(base, "/resume"), body); err != nil {
			var statusErr *serveHTTPStatusError
			if errors.As(err, &statusErr) {
				if !a.rollbackRemoteTabProvisionalResume(tabID, tab, client, gen, route) {
					// A newer route already superseded this request. Its identity is
					// authoritative, so the open-selection rollback must not run.
					return true
				}
				if remoteSessionTransitionBusy(err) {
					a.transitionRemoteTabState(tabID, gen, "ready", "ready", "Finish the current turn before switching sessions.")
					return false
				}
				// A received HTTP rejection is definitive: Serve did not commit the
				// target, so the previous ready route remains authoritative.
				a.transitionRemoteTabState(tabID, gen, "ready", "ready", err.Error())
				return false
			}
			// A transport failure is ambiguous: Serve may have committed the
			// resume before the tunnel lost its response. Query its current route
			// before deciding whether to commit or restore local state.
			reconcileCtx, reconcileCancel := context.WithTimeout(context.Background(), 3*time.Second)
			current, reconcileErr := serveCurrentSession(reconcileCtx, client, base)
			reconcileCancel()
			if reconcileErr != nil || current.Path == "" {
				// Do not publish either transcript from an unconfirmed generation.
				// A fresh attach resolves Serve's current session before ready.
				if startRetry := a.reconnectRemoteTabGeneration(tabID, gen); startRetry {
					a.goRemoteTabSafe("remoteTabResumeReattach", func() { a.reattachRemoteTab(tabID) })
				}
				return true
			}
			if current.Path != target.Path {
				return a.reconcileRemoteTabRejectedResume(tabID, tab, client, gen, route, current, err)
			}
			if target.Name == "" {
				target.Name = current.Name
			}
			if target.Title == "" {
				target.Title = current.Title
			}
			target.Running = target.Running || current.Running
		}
		title := strings.TrimSpace(target.Title)
		if title == "" {
			title = name
		}
		if !a.commitAndPublishRemoteTabResume(tabID, tab, client, gen, route, target, title) {
			// A newer route won the publication fence; never restore the older
			// selection over it.
			return true
		}
		a.goRemoteTabSafe("remoteTabResumeStatus", func() { _, _ = a.RemoteTabStatus(tabID) })
		return true
	}
	a.transitionRemoteTabState(tabID, gen, "ready", "ready", fmt.Sprintf("remote session %q not found", name))
	return false
}

func (a *App) SetRemoteSessionPinned(hostID, workspace, name string, pinned bool) error {
	return setRemoteSessionPinned(hostID, workspace, name, pinned)
}

func (a *App) SetRemoteProjectTitle(hostID, workspace, title string) error {
	return editUserConfig(func(c *config.Config) error {
		entry, ok := c.RemoteProject(hostID, workspace)
		if !ok {
			return fmt.Errorf("remote project %s:%s is not pinned", hostID, workspace)
		}
		entry.Title = strings.TrimSpace(title)
		return c.UpsertRemoteProject(entry)
	})
}

func (a *App) DeleteRemoteProjectSession(hostID, workspace, name string) error {
	client, base, done, err := a.serveClientForRef(hostID, workspace)
	if err != nil {
		return err
	}
	defer done()
	ctx, cancel := commandContext(a)
	defer cancel()
	body, _ := json.Marshal(map[string]string{"name": name})
	return servePost(ctx, client, serveURL(base, "/delete-session"), body)
}

func (a *App) remoteTabPost(tabID, path string, body map[string]any) error {
	client, base, expectedPath, err := a.remoteTabCommandTarget(tabID)
	if err != nil {
		return err
	}
	ctx, cancel := commandContext(a)
	defer cancel()
	var payload []byte
	if body != nil {
		payload, _ = json.Marshal(body)
	}
	return servePostForSession(ctx, client, serveURL(base, path), payload, expectedPath)
}

func (a *App) remoteTabGet(tabID, path string) (json.RawMessage, error) {
	client, base, err := a.remoteTabCommandClient(tabID)
	if err != nil {
		return nil, err
	}
	ctx, cancel := commandContext(a)
	defer cancel()
	return serveGet(ctx, client, serveURL(base, path))
}

func (a *App) SetRemoteTabModel(tabID, ref string) error {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil
	}
	a.remoteTabModelMu.Lock()
	defer a.remoteTabModelMu.Unlock()
	a.remoteTabMu.Lock()
	tab := a.remoteTabs[tabID]
	if tab == nil {
		a.remoteTabMu.Unlock()
		return fmt.Errorf("remote tab %q is not connected", tabID)
	}
	hostID := tab.ref.HostID
	workspace := tab.ref.Workspace
	currentModel := tab.model
	expectedPath := tab.routing.currentPath
	expectedGen := tab.gen
	client, base := tab.client, tab.base
	usable := client != nil && tab.state == "ready"
	a.remoteTabMu.Unlock()
	localProxy := a.remoteTabLocalProxy(tabID)

	next := ref
	if localProxy {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		if strings.TrimSpace(currentModel) == "" {
			currentModel = strings.TrimSpace(cfg.DefaultModel)
		}
		entry, ok := cfg.ResolveModel(ref)
		if !ok {
			return fmt.Errorf("unknown model %q", ref)
		}
		if !modelProviderAccessAllowed(cfg.Desktop.ProviderAccess, entry.Name) {
			return fmt.Errorf("model %q is not available", ref)
		}
		currentEntry, currentOK := cfg.ResolveModel(currentModel)
		currentKind := "openai"
		if currentOK && strings.TrimSpace(currentEntry.Kind) != "" {
			currentKind = strings.TrimSpace(currentEntry.Kind)
		}
		nextKind := strings.TrimSpace(entry.Kind)
		if nextKind == "" {
			nextKind = "openai"
		}
		if !strings.EqualFold(currentKind, nextKind) {
			return fmt.Errorf("model %q uses %s protocol; this remote session is running %s and must be restarted to change protocol", ref, nextKind, currentKind)
		}
		canonical := entry.Name + "/" + entry.Model
		if _, err := resolveProxyProvider(cfg, canonical); err != nil {
			return err
		}
		if !usable {
			return fmt.Errorf("remote tab %q is not connected", tabID)
		}
		rt, err := a.remoteRT()
		if err != nil {
			return err
		}
		ctx, cancel := commandContext(a)
		defer cancel()
		if err := rt.SwitchCredentialProxyModel(ctx, hostID, workspace, currentModel, canonical, expectedPath); err != nil {
			return err
		}
		next = canonical
	} else {
		if !usable {
			return fmt.Errorf("remote tab %q is not connected", tabID)
		}
		payload, _ := json.Marshal(map[string]any{"ref": ref})
		ctx, cancel := commandContext(a)
		defer cancel()
		if err := servePostForSession(ctx, client, serveURL(base, "/model"), payload, expectedPath); err != nil {
			return err
		}
	}

	tab.routeEventMu.Lock()
	defer tab.routeEventMu.Unlock()
	a.remoteTabMu.Lock()
	current := a.remoteTabs[tabID]
	if current != tab || current.client != client || current.gen != expectedGen {
		a.remoteTabMu.Unlock()
		return fmt.Errorf("remote tab %q closed while switching model", tabID)
	}
	if current.routing.currentPath != expectedPath {
		// The fenced request changed the session that was visible when it began,
		// but another client has since promoted a newer foreground route. Do not
		// label that newer session with the older session's model response.
		a.remoteTabMu.Unlock()
		return nil
	}
	current.model = next
	current.modelSeq = remoteTabModelSeq.Add(1)
	meta := remoteTabMetaLocked(current)
	a.remoteTabMu.Unlock()
	a.saveTabsFromRemote()
	a.emitRemoteEvent("remote-tab:updated", meta)
	return nil
}

func (a *App) remoteTabLocalProxy(tabID string) bool {
	hostID, ok := a.remoteTabHostID(tabID)
	if !ok {
		return false
	}
	cfg, err := config.Load()
	if err != nil {
		return false
	}
	host, ok := cfg.RemoteHost(hostID)
	return ok && host.CredentialProxyEnabled()
}

func (a *App) remoteTabHostID(tabID string) (string, bool) {
	a.remoteTabMu.Lock()
	defer a.remoteTabMu.Unlock()
	if tab := a.remoteTabs[tabID]; tab != nil {
		return tab.ref.HostID, true
	}
	return "", false
}

func (a *App) remoteServeModelsForTab(tabID, current string) ([]ModelInfo, error) {
	raw, err := a.remoteTabGet(tabID, "/models")
	if err != nil {
		return nil, err
	}
	var payload struct {
		Models []struct {
			Ref      string `json:"ref"`
			Provider string `json:"provider"`
			Model    string `json:"model"`
			Active   bool   `json:"active"`
		} `json:"models"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}
	cur := strings.TrimSpace(current)
	out := make([]ModelInfo, 0, len(payload.Models))
	for _, entry := range payload.Models {
		ref := strings.TrimSpace(entry.Ref)
		if ref == "" {
			continue
		}
		out = append(out, ModelInfo{Ref: ref, Provider: entry.Provider, Model: entry.Model, Current: ref == cur || entry.Active})
	}
	return out, nil
}

func (a *App) SetRemoteTabEffort(tabID, level string) error {
	return a.remoteTabPost(tabID, "/effort", map[string]any{"level": level})
}

func (a *App) PauseRemoteTabGoal(tabID string) error {
	return a.remoteTabPost(tabID, "/goal/pause", nil)
}

func (a *App) ResumeRemoteTabGoal(tabID string) error {
	return a.remoteTabPost(tabID, "/goal/resume", nil)
}

func (a *App) CancelRemoteTabJobs(tabID string, jobIDs []string) error {
	return a.remoteTabPost(tabID, "/jobs/cancel", map[string]any{"ids": jobIDs})
}

func (a *App) SteerRemoteTab(tabID, input string) error {
	input = strings.TrimSpace(input)
	if input == "" {
		return fmt.Errorf("guidance is required")
	}
	return a.remoteTabPost(tabID, "/inbox/items", map[string]any{"input": input, "intent": "steer"})
}

func (a *App) SetRemoteTabPlanMode(tabID string, on bool) error {
	return a.remoteTabPost(tabID, "/plan", map[string]any{"on": on})
}

func (a *App) CompactRemoteTab(tabID, instructions string) error {
	return a.remoteTabPost(tabID, "/compact", map[string]any{"instructions": strings.TrimSpace(instructions)})
}

func (a *App) ReplayRemoteTabPrompts(tabID string) (json.RawMessage, error) {
	return a.remoteTabGet(tabID, "/pending-prompts")
}

func (a *App) ForkRemoteTab(tabID string, turn int, name string) error {
	return a.remoteTabPost(tabID, "/fork", map[string]any{"turn": turn, "name": name})
}

func (a *App) SummarizeRemoteTab(tabID string, turn int, mode string) error {
	return a.remoteTabPost(tabID, "/summarize", map[string]any{"turn": turn, "mode": mode})
}

func (a *App) ForgetRemoteTab(tabID, name string) error {
	return a.remoteTabPost(tabID, "/forget", map[string]any{"name": name})
}

func (a *App) RemoteTabBranches(tabID string) (json.RawMessage, error) {
	return a.remoteTabGet(tabID, "/branches")
}

func (a *App) RemoteTabSkills(tabID string) (json.RawMessage, error) {
	return a.remoteTabGet(tabID, "/skills")
}

func (a *App) refreshRemoteTabTitle(tabID string) {
	a.remoteTabMu.Lock()
	tab := a.remoteTabs[tabID]
	if tab == nil || tab.client == nil {
		a.remoteTabMu.Unlock()
		return
	}
	client, base, gen, expectedPath := tab.client, tab.base, tab.gen, tab.routing.currentPath
	refreshPath := expectedPath
	if refreshPath == "" {
		// An unsaved blank session has no path until its first turn materializes.
		// NUL cannot occur in a real path, so it safely keys that in-flight lookup.
		refreshPath = "\x00"
	}
	if tab.titleRefresh.path == refreshPath {
		a.remoteTabMu.Unlock()
		return
	}
	tab.titleRefresh.seq++
	refreshSeq := tab.titleRefresh.seq
	tab.titleRefresh.path = refreshPath
	a.remoteTabMu.Unlock()
	defer func() {
		a.remoteTabMu.Lock()
		if current := a.remoteTabs[tabID]; current == tab && current.titleRefresh.seq == refreshSeq {
			current.titleRefresh.path = ""
		}
		a.remoteTabMu.Unlock()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	entries, err := serveSessions(ctx, client, base)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.Current || expectedPath != "" && strings.TrimSpace(entry.Path) != strings.TrimSpace(expectedPath) {
			continue
		}
		entry.Name = strings.TrimSpace(entry.Name)
		entry.Path = strings.TrimSpace(entry.Path)
		if !a.adoptRemoteTabTitleListing(tabID, tab, client, gen, expectedPath, entry) {
			return
		}

		override, migrateErr := migrateRemoteSessionTitleOverride(tab.ref.HostID, tab.ref.Workspace, entry.Name)
		if migrateErr != nil || override == "" {
			override = remoteSessionTitleOverride(tab.ref.HostID, tab.ref.Workspace, entry.Name)
		}
		if override != "" {
			a.applyRemoteTabTitleOverride(tabID, tab, client, gen, entry, override)
		}
		return
	}
}

func (a *App) adoptRemoteTabTitleListing(tabID string, tab *remoteTab, client *http.Client, gen uint64, expectedPath string, entry serveSessionEntry) bool {
	tab.routeEventMu.Lock()
	defer tab.routeEventMu.Unlock()
	a.remoteTabMu.Lock()
	current := a.remoteTabs[tabID]
	if current != tab || current.client != client || current.gen != gen || current.routing.currentPath != expectedPath {
		a.remoteTabMu.Unlock()
		return false
	}
	// Linearize the durable identity before migrating the synthetic blank's
	// preferences. Preference I/O runs later without either application lock.
	title := strings.TrimSpace(entry.Title)
	changed := title != "" && current.topicTitle != title
	identityChanged := current.session.name != entry.Name || current.session.path != entry.Path || current.session.reset || current.session.newSession
	if changed {
		current.topicTitle = title
	}
	current.session.reset = false
	current.session.newSession = false
	current.session.name = entry.Name
	current.session.path = entry.Path
	if current.routing.currentPath != entry.Path {
		current.routing.currentPath = entry.Path
		current.routing.pathRevision++
		current.routing.revision++
	}
	meta := remoteTabMetaLocked(current)
	a.remoteTabMu.Unlock()
	if changed || identityChanged {
		a.emitRemoteEvent("remote-tab:updated", meta)
		a.saveTabsFromRemote()
	}
	return true
}

func (a *App) applyRemoteTabTitleOverride(tabID string, tab *remoteTab, client *http.Client, gen uint64, entry serveSessionEntry, override string) {
	tab.routeEventMu.Lock()
	defer tab.routeEventMu.Unlock()
	a.remoteTabMu.Lock()
	current := a.remoteTabs[tabID]
	if current != tab || current.client != client || current.gen != gen ||
		current.session.name != entry.Name || current.session.path != entry.Path ||
		current.routing.currentPath != entry.Path {
		a.remoteTabMu.Unlock()
		return
	}
	changed := current.topicTitle != override
	if changed {
		current.topicTitle = override
	}
	meta := remoteTabMetaLocked(current)
	a.remoteTabMu.Unlock()
	if changed {
		a.emitRemoteEvent("remote-tab:updated", meta)
		a.saveTabsFromRemote()
	}
}

func (a *App) resetRemoteTabSession(tabID string) error {
	return a.rotateRemoteTabSession(tabID, "/new")
}

// ClearRemoteTabSession clears the active remote transcript through Serve's
// dedicated session-rotation endpoint. It intentionally bypasses /submit so
// the frontend does not create an optimistic conversational turn for /clear.
func (a *App) ClearRemoteTabSession(tabID string) error {
	return a.rotateRemoteTabSession(tabID, "/clear")
}

func (a *App) rotateRemoteTabSession(tabID, path string) error {
	a.remoteTabMu.Lock()
	tab := a.remoteTabs[tabID]
	if tab == nil {
		a.remoteTabMu.Unlock()
		return fmt.Errorf("remote tab %q is not connected", tabID)
	}
	a.remoteTabMu.Unlock()
	tab.sessionMu.Lock()
	defer tab.sessionMu.Unlock()
	a.remoteTabMu.Lock()
	if a.remoteTabs[tabID] != tab {
		a.remoteTabMu.Unlock()
		return fmt.Errorf("remote tab %q closed while starting a new session", tabID)
	}
	if tab.client == nil {
		if path != "/new" {
			a.remoteTabMu.Unlock()
			return fmt.Errorf("remote tab %q is not connected", tabID)
		}
		tab.session.newSession = true
		tab.session.name = ""
		tab.session.path = ""
		if tab.routing.currentPath != "" {
			tab.routing.currentPath = ""
			tab.routing.pathRevision++
		}
		tab.routing.revision++
		a.remoteTabMu.Unlock()
		return nil
	}
	client, base := tab.client, tab.base
	requestPath := tab.routing.currentPath
	requestPathRevision := tab.routing.pathRevision
	a.remoteTabMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	rotatedPath, err := servePostSessionPathForSession(ctx, client, serveURL(base, path), nil, requestPath)
	if err != nil {
		// Session rotation can be rejected while the current remote turn is active.
		// That does not invalidate the attached session or its event pump, so
		// return an action error while leaving the tab ready and observable.
		return err
	}
	target := serveSessionEntry{Path: rotatedPath, Current: true}
	title := a.localizedDefaultTopicTitle()
	tab.routeEventMu.Lock()
	defer tab.routeEventMu.Unlock()
	a.remoteTabMu.Lock()
	if a.remoteTabs[tabID] != tab || tab.client != client {
		a.remoteTabMu.Unlock()
		return fmt.Errorf("remote tab %q changed while starting a new session", tabID)
	}
	if tab.routing.pathRevision != requestPathRevision || tab.routing.currentPath != requestPath {
		alreadyAdopted := tab.routing.currentPath == target.Path
		a.remoteTabMu.Unlock()
		if alreadyAdopted {
			a.saveTabsFromRemote()
		}
		// A session_changed frame won the race. Its foreground identity is newer
		// than this HTTP response, even when a second rotation already moved on.
		return nil
	}
	tab.topicTitle = title
	tab.session.reset = true
	tab.session.newSession = true
	tab.session.name = target.Name
	tab.session.path = target.Path
	tab.routing.currentPath = target.Path
	tab.routing.pathRevision++
	tab.routing.revision++
	tab.pendingEvents = nil
	tab.runtime.revision++
	tab.runtime.running = false
	tab.runtime.turnStartedAt = 0
	tab.runtime.pendingPrompt = false
	tab.runtime.backgroundJobs = 0
	tab.runtime.cancelRequested = false
	tab.runtime.cancellable = false
	meta := remoteTabMetaLocked(tab)
	a.remoteTabMu.Unlock()
	a.emitRemoteEvent("remote-tab:updated", meta)
	a.saveTabsFromRemote()
	a.emitRemoteTabState(tabID, "ready", "")
	return nil
}

package main

import (
	"context"
	"encoding/json"
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
	a.remoteTabMu.Lock()
	tab := a.remoteTabs[tabID]
	a.remoteTabMu.Unlock()
	if tab == nil {
		return
	}
	tab.sessionMu.Lock()
	defer tab.sessionMu.Unlock()
	a.remoteTabMu.Lock()
	if a.remoteTabs[tabID] != tab || tab.client == nil || tab.state != "ready" {
		a.remoteTabMu.Unlock()
		return
	}
	client, base, gen := tab.client, tab.base, tab.gen
	a.remoteTabMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	entries, err := serveSessions(ctx, client, base)
	if err != nil {
		a.transitionRemoteTabState(tabID, gen, "ready", "ready", fmt.Sprintf("Could not open remote session %q: %v", name, err))
		return
	}
	for _, entry := range entries {
		if entry.Name != name {
			continue
		}
		body, _ := json.Marshal(map[string]string{"path": entry.Path})
		if err := servePost(ctx, client, serveURL(base, "/resume"), body); err != nil {
			if remoteSessionTransitionBusy(err) {
				a.transitionRemoteTabState(tabID, gen, "ready", "ready", "Finish the current turn before switching sessions.")
				return
			}
			// /resume is an action on an already attached Serve. Any rejection
			// leaves that current session and event pump usable, so surface the
			// action error without replacing the ready transcript.
			a.transitionRemoteTabState(tabID, gen, "ready", "ready", err.Error())
			return
		}
		title := strings.TrimSpace(entry.Title)
		if title == "" {
			title = name
		}
		a.remoteTabMu.Lock()
		current := a.remoteTabs[tabID]
		if current != tab || current.client != client || current.gen != gen || current.state != "ready" {
			a.remoteTabMu.Unlock()
			return
		}
		current.topicTitle = title
		current.session.reset = false
		current.session.newSession = false
		current.session.name = name
		current.runtime.revision++
		meta := remoteTabMetaLocked(current)
		a.remoteTabMu.Unlock()
		a.emitRemoteEvent("remote-tab:updated", meta)
		a.saveTabsFromRemote()
		a.transitionRemoteTabState(tabID, gen, "ready", "ready", "")
		return
	}
	a.transitionRemoteTabState(tabID, gen, "ready", "ready", fmt.Sprintf("remote session %q not found", name))
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
	client, base, err := a.remoteTabCommandClient(tabID)
	if err != nil {
		return err
	}
	ctx, cancel := commandContext(a)
	defer cancel()
	var payload []byte
	if body != nil {
		payload, _ = json.Marshal(body)
	}
	return servePost(ctx, client, serveURL(base, path), payload)
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
		rt, err := a.remoteRT()
		if err != nil {
			return err
		}
		ctx, cancel := commandContext(a)
		defer cancel()
		if err := rt.SwitchCredentialProxyModel(ctx, hostID, workspace, currentModel, canonical); err != nil {
			return err
		}
		next = canonical
	} else if err := a.remoteTabPost(tabID, "/model", map[string]any{"ref": ref}); err != nil {
		return err
	}

	a.remoteTabMu.Lock()
	current := a.remoteTabs[tabID]
	if current != tab {
		a.remoteTabMu.Unlock()
		return fmt.Errorf("remote tab %q closed while switching model", tabID)
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
	if tab == nil || tab.titleRefreshInFlight || tab.client == nil {
		a.remoteTabMu.Unlock()
		return
	}
	tab.titleRefreshInFlight = true
	client, base := tab.client, tab.base
	a.remoteTabMu.Unlock()
	defer func() {
		a.remoteTabMu.Lock()
		if tab := a.remoteTabs[tabID]; tab != nil {
			tab.titleRefreshInFlight = false
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
		if !entry.Current {
			continue
		}
		title := strings.TrimSpace(entry.Title)
		// A manual rename made while the row was the synthetic blank is
		// keyed by an empty session name. Once Serve exposes the durable
		// name, move that preference before choosing the displayed title.
		if override, migrateErr := migrateRemoteSessionTitleOverride(tab.ref.HostID, tab.ref.Workspace, entry.Name); migrateErr == nil && override != "" {
			title = override
		} else if override := remoteSessionTitleOverride(tab.ref.HostID, tab.ref.Workspace, entry.Name); override != "" {
			title = override
		}
		if title == "" {
			return
		}
		a.remoteTabMu.Lock()
		current := a.remoteTabs[tabID]
		if current != tab || current.client != client {
			a.remoteTabMu.Unlock()
			return
		}
		changed := current.topicTitle != title
		if changed {
			current.topicTitle = title
		}
		current.session.reset = false
		current.session.newSession = false
		current.session.name = entry.Name
		meta := remoteTabMetaLocked(current)
		a.remoteTabMu.Unlock()
		if changed {
			a.emitRemoteEvent("remote-tab:updated", meta)
			a.saveTabsFromRemote()
		}
		return
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
		a.remoteTabMu.Unlock()
		return nil
	}
	client, base := tab.client, tab.base
	a.remoteTabMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := servePost(ctx, client, serveURL(base, path), nil); err != nil {
		// Session rotation can be rejected while the current remote turn is active.
		// That does not invalidate the attached session or its event pump, so
		// return an action error while leaving the tab ready and observable.
		return err
	}
	title := a.localizedDefaultTopicTitle()
	a.remoteTabMu.Lock()
	if a.remoteTabs[tabID] != tab || tab.client != client {
		a.remoteTabMu.Unlock()
		return fmt.Errorf("remote tab %q changed while starting a new session", tabID)
	}
	tab.topicTitle = title
	tab.session.reset = true
	tab.session.newSession = true
	tab.session.name = ""
	tab.runtime.revision++
	meta := remoteTabMetaLocked(tab)
	a.remoteTabMu.Unlock()
	a.emitRemoteEvent("remote-tab:updated", meta)
	a.saveTabsFromRemote()
	a.emitRemoteTabState(tabID, "ready", "")
	return nil
}

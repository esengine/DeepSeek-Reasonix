package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

type RemoteTabSnapshot struct {
	History       json.RawMessage   `json:"history"`
	Context       json.RawMessage   `json:"context,omitempty"`
	Todos         json.RawMessage   `json:"todos,omitempty"`
	Checkpoints   json.RawMessage   `json:"checkpoints,omitempty"`
	Models        json.RawMessage   `json:"models,omitempty"`
	Commands      json.RawMessage   `json:"commands,omitempty"`
	Status        json.RawMessage   `json:"status,omitempty"`
	PendingEvents []json.RawMessage `json:"pendingEvents,omitempty"`
}

// RemoteTabSnapshot merges the serve's GET members in parallel. Only
// /history is required; the optional members degrade to absent on failure.
func (a *App) RemoteTabSnapshot(tabID string) (RemoteTabSnapshot, error) {
	client, base, err := a.remoteTabCommandClient(tabID)
	if err != nil {
		return RemoteTabSnapshot{}, err
	}
	gen := a.remoteTabClientGeneration(tabID, client)
	statusSeq := a.reserveRemoteTabStatusSequence(tabID, client, gen)
	ctx, cancel := commandContext(a)
	defer cancel()
	var snap RemoteTabSnapshot
	var wg sync.WaitGroup
	var mu sync.Mutex
	var historyErr error
	for path, dst := range map[string]*json.RawMessage{
		"/history":     &snap.History,
		"/context":     &snap.Context,
		"/todos":       &snap.Todos,
		"/checkpoints": &snap.Checkpoints,
		"/models":      &snap.Models,
		"/commands":    &snap.Commands,
		"/status":      &snap.Status,
	} {
		wg.Add(1)
		go func(path string, dst *json.RawMessage) {
			defer wg.Done()
			data, err := serveGet(ctx, client, serveURL(base, path))
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if path == "/history" && historyErr == nil {
					historyErr = err
				}
				return
			}
			*dst = data
		}(path, dst)
	}
	wg.Wait()
	if historyErr != nil {
		return RemoteTabSnapshot{}, historyErr
	}
	if len(snap.History) == 0 {
		return RemoteTabSnapshot{}, fmt.Errorf("remote tab %q: empty history", tabID)
	}
	a.remoteTabMu.Lock()
	tab := a.remoteTabs[tabID]
	if tab == nil || tab.client != client || tab.gen != gen || tab.state != "ready" {
		a.remoteTabMu.Unlock()
		return RemoteTabSnapshot{}, fmt.Errorf("remote tab %q changed while loading snapshot", tabID)
	}
	keys := make([]string, 0, len(tab.pendingEvents))
	for key := range tab.pendingEvents {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		snap.PendingEvents = append(snap.PendingEvents, append(json.RawMessage(nil), tab.pendingEvents[key]...))
	}
	a.remoteTabMu.Unlock()
	if len(snap.Status) > 0 && !a.recordRemoteTabSessionStatus(tabID, client, gen, statusSeq, snap.Status) {
		// Do not hand a status member captured before a newer request/event to
		// the frontend aggregate snapshot; it will fetch /status explicitly.
		snap.Status = nil
	}
	a.recordRemoteTabModelCatalog(tabID, client, gen, snap.Models)
	return snap, nil
}

// RemoteTabStatus is the small status-only binding used by watchdog and close
// policy polling. It deliberately avoids transferring full history.
func (a *App) RemoteTabStatus(tabID string) (json.RawMessage, error) {
	client, base, err := a.remoteTabCommandClient(tabID)
	if err != nil {
		return nil, err
	}
	gen := a.remoteTabClientGeneration(tabID, client)
	statusSeq := a.reserveRemoteTabStatusSequence(tabID, client, gen)
	ctx, cancel := commandContext(a)
	defer cancel()
	status, err := serveGet(ctx, client, serveURL(base, "/status"))
	if err == nil {
		if !a.recordRemoteTabSessionStatus(tabID, client, gen, statusSeq, status) {
			return nil, fmt.Errorf("remote tab %q status was superseded by newer runtime state", tabID)
		}
	}
	return status, err
}

func (a *App) remoteTabClientGeneration(tabID string, client *http.Client) uint64 {
	a.remoteTabMu.Lock()
	defer a.remoteTabMu.Unlock()
	if tab := a.remoteTabs[tabID]; tab != nil && tab.client == client {
		return tab.gen
	}
	return 0
}

func (a *App) reserveRemoteTabStatusSequence(tabID string, client *http.Client, gen uint64) uint64 {
	if gen == 0 {
		return 0
	}
	a.remoteTabMu.Lock()
	defer a.remoteTabMu.Unlock()
	tab := a.remoteTabs[tabID]
	if tab == nil || tab.client != client || tab.gen != gen {
		return 0
	}
	tab.runtime.revision++
	return tab.runtime.revision
}

func (a *App) recordRemoteTabSessionStatus(tabID string, client *http.Client, gen, statusSeq uint64, status json.RawMessage) bool {
	var payload struct {
		SessionName     string `json:"sessionName"`
		Running         *bool  `json:"running"`
		PendingPrompt   *bool  `json:"pendingPrompt"`
		BackgroundJobs  *int   `json:"backgroundJobs"`
		CancelRequested *bool  `json:"cancelRequested"`
		Cancellable     *bool  `json:"cancellable"`
	}
	if gen == 0 || statusSeq == 0 || json.Unmarshal(status, &payload) != nil {
		return false
	}
	a.remoteTabMu.Lock()
	tab := a.remoteTabs[tabID]
	if tab == nil || tab.client != client || tab.gen != gen || tab.runtime.revision != statusSeq {
		a.remoteTabMu.Unlock()
		return false
	}
	before := remoteTabMetaLocked(tab)
	if name := strings.TrimSpace(payload.SessionName); name != "" {
		tab.session.name = name
		tab.session.newSession = false
		tab.session.reset = false
	}
	if payload.Running != nil {
		tab.runtime.running = *payload.Running
	}
	if payload.PendingPrompt != nil {
		tab.runtime.pendingPrompt = *payload.PendingPrompt
	}
	if payload.BackgroundJobs != nil {
		tab.runtime.backgroundJobs = max(0, *payload.BackgroundJobs)
	}
	if payload.CancelRequested != nil {
		tab.runtime.cancelRequested = *payload.CancelRequested
	}
	if payload.Cancellable != nil {
		tab.runtime.cancellable = *payload.Cancellable
	}
	if (tab.runtime.running || tab.runtime.pendingPrompt) && tab.runtime.turnStartedAt <= 0 {
		tab.runtime.turnStartedAt = time.Now().UnixMilli()
	} else if !tab.runtime.running && !tab.runtime.pendingPrompt {
		tab.runtime.turnStartedAt = 0
	}
	after := remoteTabMetaLocked(tab)
	a.remoteTabMu.Unlock()
	if before.Running != after.Running || before.TurnStartedAt != after.TurnStartedAt ||
		before.PendingPrompt != after.PendingPrompt || before.BackgroundJobs != after.BackgroundJobs ||
		before.CancelRequested != after.CancelRequested || before.Cancellable != after.Cancellable {
		a.emitRemoteEvent("remote-tab:updated", after)
	}
	return true
}

func (a *App) recordRemoteTabModelCatalog(tabID string, client *http.Client, gen uint64, models json.RawMessage) {
	if gen == 0 || a.remoteTabLocalProxy(tabID) {
		return
	}
	var payload struct {
		Current string `json:"current"`
		Models  []struct {
			Ref    string `json:"ref"`
			Active bool   `json:"active"`
		} `json:"models"`
	}
	if json.Unmarshal(models, &payload) != nil {
		return
	}
	current := strings.TrimSpace(payload.Current)
	if current == "" {
		for _, entry := range payload.Models {
			if entry.Active {
				current = strings.TrimSpace(entry.Ref)
				break
			}
		}
	}
	if current == "" {
		return
	}
	a.remoteTabMu.Lock()
	tab := a.remoteTabs[tabID]
	if tab == nil || tab.client != client || tab.gen != gen || tab.model == current {
		a.remoteTabMu.Unlock()
		return
	}
	tab.model = current
	tab.modelSeq = remoteTabModelSeq.Add(1)
	meta := remoteTabMetaLocked(tab)
	a.remoteTabMu.Unlock()
	a.emitRemoteEvent("remote-tab:updated", meta)
	a.saveTabsFromRemote()
}

// listTabsWithRemote merges the remote strip entries into a local tab list.
// A highlighted remote tab deactivates every local entry so the strip shows
// exactly one active tab.
func (a *App) listTabsWithRemote(local []TabMeta) []TabMeta {
	localIDs := make([]string, 0, len(local))
	for _, meta := range local {
		localIDs = append(localIDs, meta.ID)
	}
	remote, remoteActive, stripOrder := a.remoteTabMetas(localIDs)
	if remoteActive != "" {
		for i := range local {
			local[i].Active = false
		}
	}
	if len(remote) == 0 {
		return enrichTabMetas(local)
	}
	all := append(enrichTabMetas(local), remote...)
	byID := make(map[string]TabMeta, len(all))
	for _, meta := range all {
		byID[meta.ID] = meta
	}
	out := make([]TabMeta, 0, len(all))
	for _, id := range stripOrder {
		if meta, ok := byID[id]; ok {
			out = append(out, meta)
		}
	}
	return out
}

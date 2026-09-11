package main

import (
	"log/slog"
	"strings"

	"reasonix/internal/agent"
	"reasonix/internal/control"
)

// kickHistoryIndexRebuild single-flight schedules a background display-index
// rebuild for a live session whose on-disk index did not validate. It never
// blocks the request path.
func (a *App) kickHistoryIndexRebuild(sessionPath string) {
	if strings.TrimSpace(sessionPath) == "" {
		return
	}
	a.historySliceMu.Lock()
	if a.historyIndexRebuilds == nil {
		a.historyIndexRebuilds = map[string]chan struct{}{}
	}
	if _, ok := a.historyIndexRebuilds[sessionPath]; ok {
		a.historySliceMu.Unlock()
		return
	}
	done := make(chan struct{})
	a.historyIndexRebuilds[sessionPath] = done
	a.historySliceMu.Unlock()
	a.goSafe("historyIndexRebuild", func() {
		defer func() {
			a.historySliceMu.Lock()
			close(done)
			delete(a.historyIndexRebuilds, sessionPath)
			a.historySliceMu.Unlock()
		}()
		a.rebuildHistoryIndexForLiveSession(sessionPath)
	})
}

// kickHistoryReadModelRepair single-flights the stronger cold-session repair:
// replay the authoritative event log under the save lock, atomically refresh
// the JSONL random-read model, then publish matching offsets. The cold request
// already returned from its in-memory recovery source before this work starts.
func (a *App) kickHistoryReadModelRepair(sessionPath string) {
	if strings.TrimSpace(sessionPath) == "" {
		return
	}
	key := "read-model:" + agent.CanonicalSessionPath(sessionPath)
	a.historySliceMu.Lock()
	if a.historyIndexRebuilds == nil {
		a.historyIndexRebuilds = map[string]chan struct{}{}
	}
	if _, ok := a.historyIndexRebuilds[key]; ok {
		a.historySliceMu.Unlock()
		return
	}
	done := make(chan struct{})
	a.historyIndexRebuilds[key] = done
	a.historySliceMu.Unlock()
	a.goSafe("historyReadModelRepair", func() {
		defer func() {
			a.historySliceMu.Lock()
			close(done)
			delete(a.historyIndexRebuilds, key)
			a.historySliceMu.Unlock()
		}()
		if err := agent.RepairSessionDisplayReadModel(sessionPath); err != nil {
			slog.Debug("desktop: history read-model repair failed", "path", sessionPath, "err", err)
		}
	})
}

// rebuildHistoryIndexForLiveSession republishes the display index for a live
// session, but only when the in-memory log is exactly the persisted
// transcript — an append-only tail means the next save will publish a
// covering index anyway, and a scanned .jsonl anchor cannot describe the
// event-log tail.
func (a *App) rebuildHistoryIndexForLiveSession(sessionPath string) {
	a.mu.RLock()
	ctrls := make([]control.SessionAPI, 0, len(a.tabs))
	for _, tab := range a.tabs {
		if tab != nil && tab.Ctrl != nil {
			ctrls = append(ctrls, tab.Ctrl)
		}
	}
	a.mu.RUnlock()
	var ctrl control.SessionAPI
	for _, c := range ctrls {
		if c.SessionPath() == sessionPath {
			ctrl = c
			break
		}
	}
	wc, ok := ctrl.(historyWindowController)
	if !ok {
		return
	}
	ps, ok := wc.SessionPersistedState()
	if !ok || !ps.UnchangedSincePersisted {
		return
	}
	if err := agent.RepairSessionDisplayReadModel(sessionPath); err != nil {
		slog.Debug("desktop: live history read-model rebuild failed", "path", sessionPath, "err", err)
	}
}

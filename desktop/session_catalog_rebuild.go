package main

import (
	"errors"
	"time"

	"reasonix/internal/sessioncatalog"
)

var (
	errSessionCatalogStopTimeout = errors.New("session catalog did not stop before the rebuild deadline")
	errSessionCatalogPanic       = errors.New("session catalog rebuild failed unexpectedly")
)

type sessionCatalogRebuildFlight struct {
	done chan struct{}
	err  error
}

// RebuildSessionCatalog owns the bounded, one-shot rebuild transaction. The
// replacement watcher is always an ordinary watcher and never owns rebuilding.
func (a *App) RebuildSessionCatalog() error {
	return a.rebuildSessionCatalog(5 * time.Second)
}

func (a *App) rebuildSessionCatalog(stopTimeout time.Duration) (err error) {
	if a == nil || a.shuttingDown.Load() {
		return errors.New("application is shutting down")
	}
	a.catalogRebuildMu.Lock()
	if flight := a.catalogRebuild; flight != nil {
		a.catalogRebuildMu.Unlock()
		if a.catalogRebuildJoinHook != nil {
			a.catalogRebuildJoinHook()
		}
		<-flight.done
		return flight.err
	}
	flight := &sessionCatalogRebuildFlight{done: make(chan struct{})}
	a.catalogRebuild = flight
	a.catalogRebuilding.Store(true)
	a.catalogRebuildMu.Unlock()

	defer func() {
		panicValue := recover()
		if panicValue != nil {
			flight.err = errSessionCatalogPanic
		} else {
			flight.err = err
		}
		a.catalogRebuildMu.Lock()
		a.catalogRebuild = nil
		close(flight.done)
		a.catalogRebuildMu.Unlock()
		if panicValue != nil {
			panic(panicValue)
		}
	}()
	err = a.runSessionCatalogRebuild(stopTimeout)
	return err
}

func (a *App) runSessionCatalogRebuild(stopTimeout time.Duration) error {
	status := a.currentSessionCatalogStatus()
	finishedRevision := status.Revision
	defer func() {
		if !a.shuttingDown.Load() {
			// Arm the ordinary watcher before releasing the single-flight gate so
			// another Wails rebuild cannot enter the stop/start handoff gap.
			a.startSessionCatalog()
		}
		a.catalogRebuilding.Store(false)
		if !a.shuttingDown.Load() {
			a.emitProjectTreeChangedV2(finishedRevision, nil, "catalog_rebuild_finished")
		}
	}()
	a.emitProjectTreeChangedV2(status.Revision, nil, "catalog_rebuild_started")

	// Rebuild must not race the old SQLite handle on Windows, where publishing
	// the atomic replacement can fail while that handle is still closing.
	if !a.stopSessionCatalog(stopTimeout) {
		return errSessionCatalogStopTimeout
	}
	replacement, err := sessioncatalog.RebuildWithRevisionFloor(
		a.bootContext(), sessioncatalog.DefaultPath(), a.sessionCatalogTargets(), status.Revision,
	)
	if err == nil {
		finishedRevision = max(finishedRevision, replacement.Revision)
	}
	return err
}

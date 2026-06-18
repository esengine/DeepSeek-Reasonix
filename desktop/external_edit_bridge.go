package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"reasonix/internal/control"
)

var externalEditBridgeSeq atomic.Uint64

var externalEditBridgeStore = struct {
	mu    sync.Mutex
	items map[string]*control.ExternalEdit
}{items: map[string]*control.ExternalEdit{}}

type externalEditBridge struct {
	edit *control.ExternalEdit
}

// BeginExternalEditForTab starts a host-side edit bridge and returns an opaque
// handle for EndExternalEdit. It is exported so Wails or another host shim can
// wrap external patch runners without exposing function-valued bindings.
func (a *App) BeginExternalEditForTab(tabID, label string, paths []string) (string, error) {
	bridge, err := a.beginExternalEditForTab(tabID, label, paths)
	if err != nil {
		return "", err
	}
	id := fmt.Sprintf("external-edit-%d", externalEditBridgeSeq.Add(1))
	externalEditBridgeStore.mu.Lock()
	externalEditBridgeStore.items[id] = bridge.edit
	externalEditBridgeStore.mu.Unlock()
	return id, nil
}

// EndExternalEdit finishes a handle returned by BeginExternalEditForTab.
// errMessage is empty on success; non-empty marks the synthetic tool result as
// failed and avoids recording a checkpoint for any partial writes.
func (a *App) EndExternalEdit(id, errMessage string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("external edit id is required")
	}
	externalEditBridgeStore.mu.Lock()
	edit := externalEditBridgeStore.items[id]
	delete(externalEditBridgeStore.items, id)
	externalEditBridgeStore.mu.Unlock()
	if edit == nil {
		return fmt.Errorf("external edit %q not found", id)
	}
	var err error
	if msg := strings.TrimSpace(errMessage); msg != "" {
		err = errors.New(msg)
	}
	return edit.End(err)
}

func (a *App) beginExternalEditForTab(tabID, label string, paths []string) (*externalEditBridge, error) {
	if a.tabReadOnly(tabID) {
		return nil, fmt.Errorf("tab is read-only")
	}
	ctrl := a.ctrlByTabID(tabID)
	if ctrl == nil {
		return nil, fmt.Errorf("tab %q not found", strings.TrimSpace(tabID))
	}
	edit := ctrl.BeginExternalEdit(label, paths)
	if err := edit.BeginErr(); err != nil {
		return nil, fmt.Errorf("cannot start external edit: %w", err)
	}
	return &externalEditBridge{edit: edit}, nil
}

func (b *externalEditBridge) End(err error) error {
	if b == nil || b.edit == nil {
		return err
	}
	return b.edit.End(err)
}

func (a *App) runExternalEditForTab(tabID, label string, paths []string, fn func(context.Context) error) error {
	if a.tabReadOnly(tabID) {
		return fmt.Errorf("tab is read-only")
	}
	ctrl := a.ctrlByTabID(tabID)
	if ctrl == nil {
		return fmt.Errorf("tab %q not found", strings.TrimSpace(tabID))
	}
	return ctrl.RunExternalEdit(context.Background(), label, paths, fn)
}

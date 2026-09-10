package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"reasonix/internal/control"
)

type toolRecoveryController interface {
	ToolRecoverySnapshot() control.ToolRecoverySnapshot
	ResolveToolRecovery(context.Context, control.ToolRecoveryRequest) (control.ToolRecoverySnapshot, error)
}

// Both local and remote surfaces use the same generation-bound request shape.
func (a *App) GetToolRecoveryForTab(tabID string) (control.ToolRecoverySnapshot, error) {
	if a.isRemoteTab(tabID) {
		return a.remoteToolRecovery(tabID, nil)
	}
	_, ctrl := a.tabAndCtrlByID(tabID)
	if target, ok := ctrl.(toolRecoveryController); ok {
		return target.ToolRecoverySnapshot(), nil
	}
	return control.ToolRecoverySnapshot{}, fmt.Errorf("tool recovery unavailable")
}

func (a *App) ResolveToolRecoveryForTab(tabID string, req control.ToolRecoveryRequest) (control.ToolRecoverySnapshot, error) {
	if a.isRemoteTab(tabID) {
		return a.remoteToolRecovery(tabID, &req)
	}
	_, ctrl := a.tabAndCtrlByID(tabID)
	if target, ok := ctrl.(toolRecoveryController); ok {
		ctx, cancel := commandContext(a)
		defer cancel()
		return target.ResolveToolRecovery(ctx, req)
	}
	return control.ToolRecoverySnapshot{}, fmt.Errorf("tool recovery unavailable")
}

func (a *App) remoteToolRecovery(tabID string, req *control.ToolRecoveryRequest) (control.ToolRecoverySnapshot, error) {
	client, base, path, err := a.remoteTabCommandTarget(tabID)
	if err != nil {
		return control.ToolRecoverySnapshot{}, err
	}
	ctx, cancel := commandContext(a)
	defer cancel()
	method := http.MethodGet
	var body []byte
	if req != nil {
		method = http.MethodPost
		body, err = json.Marshal(req)
		if err != nil {
			return control.ToolRecoverySnapshot{}, err
		}
	}
	resp, err := serveDoForSession(ctx, client, method, serveURL(base, "/tool-recovery"), body, path)
	if err != nil {
		return control.ToolRecoverySnapshot{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return control.ToolRecoverySnapshot{}, fmt.Errorf("tool recovery: %s", b)
	}
	var view control.ToolRecoverySnapshot
	err = json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&view)
	if err == nil && view.SessionPath != path {
		return view, fmt.Errorf("remote recovery session changed")
	}
	return view, err
}

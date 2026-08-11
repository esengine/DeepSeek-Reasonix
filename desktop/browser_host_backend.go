package main

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"reasonix/desktop/internal/browseripc"
	"reasonix/internal/browserhost"
	"reasonix/internal/extension/protocol"
)

// tabBrowserBackend binds BrowserCoordinator operations to one desktop chat
// tab (ownerId). It never accepts ownerId from plugins — the binding is fixed
// when the controller is built.
type tabBrowserBackend struct {
	app     *App
	ownerID string
}

// browserHostForTab returns a restricted browserhost.Backend for the chat tab.
// Nil when the app has no coordinator (should not happen on desktop).
func (a *App) browserHostForTab(tabID string) browserhost.Backend {
	if a == nil || a.browser == nil || strings.TrimSpace(tabID) == "" {
		return nil
	}
	return &tabBrowserBackend{app: a, ownerID: tabID}
}

func (b *tabBrowserBackend) List(ctx context.Context) ([]protocol.BrowserTab, error) {
	var res browseripc.TabListResult
	if err := b.app.browser.Call(ctx, b.ownerID, "tab.list", browseripc.OwnerParams{OwnerID: b.ownerID}, &res); err != nil {
		return nil, mapBrowserIPCError(err)
	}
	out := make([]protocol.BrowserTab, 0, len(res.Tabs))
	for _, t := range res.Tabs {
		out = append(out, tabInfoToProtocol(t))
	}
	return out, nil
}

func (b *tabBrowserBackend) Open(ctx context.Context, p protocol.BrowserTabOpenParams) (protocol.BrowserTab, error) {
	disp := browseripc.DispositionForeground
	if p.Disposition == protocol.BrowserDispositionBackground {
		disp = browseripc.DispositionBackground
	}
	var res browseripc.TabInfo
	if err := b.app.browser.Call(ctx, b.ownerID, "tab.open", browseripc.TabOpenParams{
		OwnerID: b.ownerID, URL: p.URL, Disposition: disp, FromAgent: true,
	}, &res); err != nil {
		return protocol.BrowserTab{}, mapBrowserIPCError(err)
	}
	return tabInfoToProtocol(res), nil
}

func (b *tabBrowserBackend) Snapshot(ctx context.Context, p protocol.BrowserTabSnapshotParams) (protocol.BrowserTabSnapshotResult, error) {
	maxChars := browseripc.MaxTextChars
	if p.MaxChars != nil && *p.MaxChars > 0 && *p.MaxChars < maxChars {
		maxChars = *p.MaxChars
	}
	var res browseripc.TabSnapshotResult
	if err := b.app.browser.Call(ctx, b.ownerID, "tab.snapshot", browseripc.TabSnapshotParams{
		OwnerID: b.ownerID, TabID: p.TabID, MaxChars: &maxChars,
	}, &res); err != nil {
		return protocol.BrowserTabSnapshotResult{}, mapBrowserIPCError(err)
	}
	snapshot := res.Snapshot.Tree
	if snapshot == "" {
		snapshot = res.Snapshot.Text
	} else if res.Snapshot.Text != "" {
		snapshot = snapshot + "\n" + res.Snapshot.Text
	}
	return protocol.BrowserTabSnapshotResult{
		Tab: protocol.BrowserTab{
			TabID: res.TabID, URL: res.URL, Title: res.Title,
			Active: false, Generation: uint64(res.Generation),
		},
		Origin:   originFromURL(res.URL),
		Snapshot: snapshot,
	}, nil
}

func (b *tabBrowserBackend) Wait(ctx context.Context, p protocol.BrowserTabWaitParams) (protocol.BrowserTab, error) {
	waitUntil := browseripc.WaitLoad
	switch p.WaitUntil {
	case protocol.BrowserWaitNetworkIdle:
		waitUntil = browseripc.WaitNetworkIdle
	case protocol.BrowserWaitDOMContentLoaded:
		waitUntil = browseripc.WaitDOMContent
	case protocol.BrowserWaitNavigation:
		waitUntil = browseripc.WaitNavigation
	}
	var res browseripc.TabWaitResult
	if err := b.app.browser.Call(ctx, b.ownerID, "tab.wait", browseripc.TabWaitParams{
		OwnerID: b.ownerID, TabID: p.TabID, WaitUntil: waitUntil, TimeoutMs: p.TimeoutMillis,
	}, &res); err != nil {
		return protocol.BrowserTab{}, mapBrowserIPCError(err)
	}
	return protocol.BrowserTab{
		TabID: res.TabID, URL: res.URL, Title: res.Title,
		Active: false, Generation: uint64(res.Generation),
	}, nil
}

func (b *tabBrowserBackend) Act(ctx context.Context, p protocol.BrowserTabActParams) (protocol.BrowserTab, error) {
	params := browseripc.TabActParams{
		OwnerID:        b.ownerID,
		TabID:          p.TabID,
		ExpectedOrigin: p.ExpectedOrigin,
		Action:         browseripc.ActAction(p.Action),
		Ref:            p.Ref,
		Text:           p.Text,
		Key:            p.Key,
		Value:          p.Value,
		Delta:          p.Delta,
	}
	var res browseripc.TabActResult
	if err := b.app.browser.Call(ctx, b.ownerID, "tab.act", params, &res); err != nil {
		return protocol.BrowserTab{}, mapBrowserIPCError(err)
	}
	return protocol.BrowserTab{
		TabID: res.TabID, URL: res.URL, Title: res.Title,
		Active: false, Generation: uint64(res.Generation),
	}, nil
}

func tabInfoToProtocol(t browseripc.TabInfo) protocol.BrowserTab {
	return protocol.BrowserTab{
		TabID: t.TabID, URL: t.URL, Title: t.Title,
		Active: t.Active, Generation: uint64(t.Generation),
	}
}

func originFromURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

func mapBrowserIPCError(err error) error {
	if err == nil {
		return nil
	}
	var codeErr *browserCodeError
	if errors.As(err, &codeErr) && codeErr != nil {
		switch codeErr.code {
		case browseripc.CodeComponentMissing, browseripc.CodeNotReady, browseripc.CodeCrashed, browseripc.CodeUnsupported:
			return protocol.MustProtocolError(protocol.ErrBrowserUnavailable)
		case browseripc.CodeTabNotFound, browseripc.CodeOwnerNotFound:
			return protocol.MustProtocolError(protocol.ErrBrowserTabNotFound)
		case browseripc.CodeTabBusy:
			return protocol.MustProtocolError(protocol.ErrBrowserTabBusy)
		case browseripc.CodeStaleRef:
			return protocol.MustProtocolError(protocol.ErrBrowserStaleRef)
		case browseripc.CodePermissionDenied, browseripc.CodeUserTakeoverReq, browseripc.CodeUserTookControl:
			return protocol.MustProtocolError(protocol.ErrBrowserPermissionDenied)
		case browseripc.CodeTimeout:
			return protocol.MustProtocolError(protocol.ErrBrowserTimeout)
		case browseripc.CodeCancelled:
			return protocol.MustProtocolError(protocol.ErrBrowserCancelled)
		case browseripc.CodeInvalidParams:
			return protocol.MustProtocolError(protocol.ErrInvalidParams)
		default:
			return protocol.MustProtocolError(protocol.ErrInternal)
		}
	}
	if errors.Is(err, ErrBrowserComponentMissing) || errors.Is(err, ErrBrowserDisabled) || errors.Is(err, ErrBrowserNotReady) {
		return protocol.MustProtocolError(protocol.ErrBrowserUnavailable)
	}
	if errors.Is(err, context.Canceled) {
		return protocol.MustProtocolError(protocol.ErrBrowserCancelled)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return protocol.MustProtocolError(protocol.ErrBrowserTimeout)
	}
	msg := err.Error()
	if strings.Contains(msg, "origin") {
		return protocol.MustProtocolError(protocol.ErrBrowserOriginMismatch)
	}
	return &protocol.ProtocolError{Reason: protocol.ErrInternal, Message: fmt.Sprintf("browser backend error: %s", msg)}
}

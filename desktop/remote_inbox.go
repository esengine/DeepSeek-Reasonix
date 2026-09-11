package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"reasonix/internal/agent"
	"reasonix/internal/sessioninbox"
)

// remoteInboxSnapshot reads the selected durable queue using the same route
// identity as remote runtime synchronization. A reply from a replaced tunnel
// or selection must not clear or populate the current composer's queue.
func (a *App) remoteInboxSnapshot(tabID string) (InboxSnapshotView, error) {
	a.remoteTabMu.Lock()
	tab := a.remoteTabs[tabID]
	if tab == nil || tab.client == nil || tab.state != "ready" || tab.routing.currentPath == "" || tab.routing.rehydratingPath != "" {
		a.remoteTabMu.Unlock()
		return InboxSnapshotView{}, fmt.Errorf("remote tab %q is not ready for inbox reads", tabID)
	}
	client, base, path := tab.client, tab.base, tab.routing.currentPath
	gen, selection := tab.gen, tab.selectionRevision
	a.remoteTabMu.Unlock()

	ctx, cancel := commandContext(a)
	defer cancel()
	data, err := serveGet(ctx, client, serveURL(base, "/inbox?session="+url.QueryEscape(path)))
	if err != nil {
		return InboxSnapshotView{}, err
	}
	var snap sessioninbox.InboxSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return InboxSnapshotView{}, err
	}
	if snap.SessionPath == "" || agent.CanonicalSessionPath(snap.SessionPath) != agent.CanonicalSessionPath(path) {
		return InboxSnapshotView{}, fmt.Errorf("remote inbox session changed")
	}
	a.remoteTabMu.Lock()
	defer a.remoteTabMu.Unlock()
	current := a.remoteTabs[tabID]
	if current != tab || current.gen != gen || current.selectionRevision != selection || current.client != client || current.base != base || current.state != "ready" || current.routing.currentPath != path || current.routing.rehydratingPath != "" {
		return InboxSnapshotView{}, fmt.Errorf("remote inbox route changed during read")
	}
	return inboxSnapshotView(snap), nil
}

// enqueueRemoteFollowup preserves the route, rich input and caller's stable
// key. An uncertain POST is reconciled by reading its receipt, never replayed.
func (a *App) enqueueRemoteFollowup(tabID, display, submit string, invocations []InvocationRequest, idempotency string) (InboxReceiptView, error) {
	client, base, path, err := a.remoteTabCommandTarget(tabID)
	if err != nil {
		return InboxReceiptView{}, err
	}
	return a.enqueueRemoteFollowupAt(client, base, path, display, submit, invocations, idempotency)
}

func (a *App) enqueueRemoteFollowupAt(client *http.Client, base, path, display, submit string, invocations []InvocationRequest, idempotency string) (InboxReceiptView, error) {
	ctx, cancel := commandContext(a)
	defer cancel()
	body, err := json.Marshal(map[string]any{"intent": "followup", "input": submit, "display": display, "invocations": invocations, "idempotencyKey": idempotency})
	if err != nil {
		return InboxReceiptView{}, err
	}
	resp, err := serveDoForSession(ctx, client, http.MethodPost, serveURL(base, "/inbox/items"), body, path)
	if err == nil {
		defer resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			var receipt InboxReceiptView
			err = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&receipt)
			if err == nil && receipt.ItemID != "" {
				return receipt, nil
			}
		} else {
			err = fmt.Errorf("follow-up enqueue failed (%d)", resp.StatusCode)
			if resp.StatusCode >= 400 && resp.StatusCode < 500 {
				if resp.StatusCode != http.StatusRequestTimeout {
					return InboxReceiptView{}, inboxNotSubmitted(err)
				}
				return InboxReceiptView{}, err
			}
		}
	}
	if idempotency != "" {
		lookupCtx, lookupCancel := commandContext(a)
		defer lookupCancel()
		data, lookupErr := serveGet(lookupCtx, client, serveURL(base, "/inbox/receipt?key="+url.QueryEscape(idempotency)+"&session="+url.QueryEscape(path)))
		var receipt InboxReceiptView
		if lookupErr == nil && json.Unmarshal(data, &receipt) == nil && receipt.ItemID != "" {
			return receipt, nil
		}
	}
	if err == nil {
		err = fmt.Errorf("follow-up receipt unavailable")
	}
	return InboxReceiptView{}, err
}

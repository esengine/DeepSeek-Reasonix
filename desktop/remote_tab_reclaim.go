package main

import (
	"context"
	"net/http"
	"time"
)

func takeoverViewLocallyOwned(view SessionTakeoverView) bool {
	return view.Mirrored || view.Holder == "external" || view.Holder == "other"
}

// reconcileRemoteTabReclaimOwnership keeps an ambiguous reclaim response from
// changing input authority. Only a successful, generation-fenced ownership
// probe may update the spectator pin.
func (a *App) reconcileRemoteTabReclaimOwnership(
	tabID string,
	client *http.Client,
	base, expectedPath string,
	stillCurrent func(*remoteTab) bool,
) {
	a.goRemoteTabSafe("reclaimOwnershipProbe", func() {
		probeCtx, probeCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer probeCancel()
		view, err := takeoverOwnership(probeCtx, client, base, expectedPath)
		if err != nil {
			return
		}
		locallyOwned := takeoverViewLocallyOwned(view)
		a.remoteTabMu.Lock()
		current := a.remoteTabs[tabID]
		if !stillCurrent(current) || current.session.takenOver == locallyOwned {
			a.remoteTabMu.Unlock()
			return
		}
		current.session.takenOver = locallyOwned
		meta := remoteTabMetaLocked(current)
		a.remoteTabMu.Unlock()
		a.emitRemoteEvent("remote-tab:updated", meta)
	})
}

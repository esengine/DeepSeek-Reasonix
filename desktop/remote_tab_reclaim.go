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
		blocked := view.Holder == "other" && !view.Mirrored && view.Reclaimable != nil && !*view.Reclaimable
		a.remoteTabMu.Lock()
		current := a.remoteTabs[tabID]
		holderPID, holderHost, holderKind, holderWriterID := 0, "", "", ""
		if locallyOwned {
			holderPID = view.HolderPID
			holderHost = view.HolderHost
			holderKind = view.HolderKind
			holderWriterID = view.HolderWriterID
		}
		if !stillCurrent(current) || (current.session.takenOver == locallyOwned &&
			current.session.reclaimBlocked == blocked && current.session.holderPID == holderPID &&
			current.session.holderHost == holderHost && current.session.holderKind == holderKind &&
			current.session.holderWriterID == holderWriterID) {
			a.remoteTabMu.Unlock()
			return
		}
		current.session.takenOver = locallyOwned
		current.session.reclaimBlocked = blocked
		current.session.holderPID = holderPID
		current.session.holderHost = holderHost
		current.session.holderKind = holderKind
		current.session.holderWriterID = holderWriterID
		meta := remoteTabMetaLocked(current)
		a.remoteTabMu.Unlock()
		a.emitRemoteEvent("remote-tab:updated", meta)
	})
}

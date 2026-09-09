package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"reasonix/internal/config"
)

type credentialProxyOwnership struct {
	config.ModelSettingsOwnership
	pending map[string]string
	latest  remoteModelSettingsStatus
}

func (a *App) installRemoteModelSettingsSnapshot(ctx context.Context, client *http.Client, base, host, workspace, path, ref string, bundle *config.ModelRuntimeSettings, prior remoteModelSettingsStatus) (remoteModelSettingsStatus, error) {
	if !a.reserveCredentialProxyInstall(host, workspace, bundle.OfferID, bundle.Revision, prior.OwnershipIncarnation) {
		a.finishCredentialProxyOffer(host, workspace, bundle.OfferID)
		return prior, fmt.Errorf("remote ownership changed while preparing model settings")
	}
	status, err := applyRemoteModelSettingsSnapshot(ctx, client, base, path, ref, bundle, prior)
	if err != nil {
		if status.OwnershipIncarnation == prior.OwnershipIncarnation && status.OwnershipSeq > prior.OwnershipSeq {
			prior = status
		}
		var rejected *remoteModelSettingsRejection
		if errors.As(err, &rejected) {
			a.reconcileCredentialProxyGenerations(host, workspace, prior, bundle.OfferID)
		} else {
			a.reconcileCredentialProxyGenerations(host, workspace, prior)
		}
		return status, err
	}
	a.reconcileCredentialProxyGenerations(host, workspace, status, bundle.OfferID)
	return status, nil
}

func (a *App) reserveCredentialProxyInstall(host, workspace, offer, revision, incarnation string) bool {
	a.credProxyMu.Lock()
	p := a.credProxy
	a.credProxyMu.Unlock()
	if p == nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if owner := p.ownership[credentialProxyScope(host, workspace)]; owner != nil && owner.OwnershipIncarnation == incarnation {
		if owner.pending == nil {
			owner.pending = map[string]string{}
		}
		owner.pending[offer] = revision
		return true
	}
	return false
}

func (m *desktopRemoteManager) pinModelSettingsOwnership(app *App, host, workspace string, managed *managedHost, status remoteModelSettingsStatus) bool {
	// No route-lock holder calls into the manager. Keep identity validation and
	// pin together so a replaced connection cannot restore an old incarnation.
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.hosts[host] == managed && app.pinCredentialProxyOwnership(host, workspace, status)
}

// Only the authenticated GET performed under the current managed connection's
// serve gate may establish a Serve incarnation. Source calls cannot replace it.
func (a *App) pinCredentialProxyOwnership(host, workspace string, status remoteModelSettingsStatus) bool {
	if status.OwnershipIncarnation == "" || status.OwnershipSeq == 0 {
		return false
	}
	a.credProxyMu.Lock()
	p := a.credProxy
	a.credProxyMu.Unlock()
	if p == nil {
		return false
	}
	p.updateMu.Lock()
	defer p.updateMu.Unlock()
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ownership == nil {
		p.ownership = map[string]*credentialProxyOwnership{}
	}
	scope := credentialProxyScope(host, workspace)
	if old := p.ownership[scope]; old == nil || old.OwnershipIncarnation != status.OwnershipIncarnation {
		// A new Serve cannot own an unfinished build in the previous process.
		// Preserve routes until its full receipt is reconciled, but release all
		// old-process reservations. Late builders recheck incarnation below.
		if old != nil {
			for _, route := range p.routes {
				if route.scope == scope {
					clear(route.holds)
				}
			}
		}
		p.ownership[scope] = &credentialProxyOwnership{ModelSettingsOwnership: config.ModelSettingsOwnership{OwnershipIncarnation: status.OwnershipIncarnation}}
	}
	return true
}

// Retirement, receipt ordering, and offer release share one route transaction.
// An accepted HTTP request retains its route until it returns. An uncertain
// install retains its hold until a receipt positively identifies that revision.
func (a *App) reconcileCredentialProxyGenerations(host, workspace string, status remoteModelSettingsStatus, offers ...string) bool {
	if status.Version != 1 {
		return false
	}
	a.credProxyMu.Lock()
	p := a.credProxy
	a.credProxyMu.Unlock()
	if p == nil {
		return false
	}
	p.updateMu.Lock()
	defer p.updateMu.Unlock()
	p.mu.Lock()
	defer p.mu.Unlock()
	scope := credentialProxyScope(host, workspace)
	authority := p.ownership[scope]
	if authority == nil || authority.OwnershipIncarnation != status.OwnershipIncarnation {
		return false
	}
	accepted := status.OwnershipSeq > authority.OwnershipSeq
	if accepted {
		authority.OwnershipSeq = status.OwnershipSeq
		authority.latest = status
		authority.latest.OwnedRevisions = append([]string(nil), status.OwnedRevisions...)
	} else {
		// A confirmed older install may release its own reservation, but only
		// the newest complete ownership snapshot may drive retirement.
		if len(offers) == 0 {
			return false
		}
		status = authority.latest
	}
	owned := map[string]bool{}
	for _, revision := range status.OwnedRevisions {
		owned[revision] = true
	}
	for offer, revision := range authority.pending {
		if owned[revision] {
			offers = append(offers, offer)
		}
	}
	for _, offer := range offers {
		delete(authority.pending, offer)
	}
	for token, route := range p.routes {
		if route.scope != scope {
			continue
		}
		for _, offer := range offers {
			delete(route.holds, offer)
		}
		if status.UnversionedOwners || owned[route.revision] || len(route.holds) > 0 {
			continue
		}
		route.retired = true
		if route.active == 0 {
			delete(p.routes, token)
		}
	}
	return accepted
}

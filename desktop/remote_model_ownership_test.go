package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"reasonix/internal/config"
)

func TestRemoteOwnershipRejectsOvertakenReceipts(t *testing.T) {
	for _, scenario := range []string{"old status", "old install", "old incarnation", "unversioned"} {
		t.Run(scenario, func(t *testing.T) {
			scope := credentialProxyScope("h", "w")
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))
			defer up.Close()
			p := &credentialProxy{routes: map[string]*credProxyRoute{}}
			app := &App{credProxy: p}
			_, err := p.resolveAndSetRoute("new-token", "p/m", func() (proxyUpstream, error) {
				return proxyUpstream{url: mustParseURL(t, up.URL), scope: scope, revision: "new", offerID: "candidate"}, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			old := remoteModelSettingsStatus{Version: 1, ModelSettingsOwnership: config.ModelSettingsOwnership{OwnershipIncarnation: "instance", OwnershipSeq: 1}, OwnedRevisions: []string{"old"}}
			if !app.pinCredentialProxyOwnership("h", "w", old) {
				t.Fatal("pin failed")
			}
			fresh := old
			fresh.OwnershipSeq = 3
			fresh.OwnedRevisions = []string{"new"}
			if scenario == "old incarnation" {
				fresh.OwnershipIncarnation = "replacement"
				app.pinCredentialProxyOwnership("h", "w", fresh)
			}
			if scenario == "unversioned" {
				fresh.UnversionedOwners = true
			}
			if !app.reconcileCredentialProxyGenerations("h", "w", fresh, "candidate") {
				t.Fatal("fresh receipt rejected")
			}
			var released []string
			if scenario == "old install" {
				p.routes["new-token"].holds["old-install"] = true
				released = []string{"old-install"}
			}
			if app.reconcileCredentialProxyGenerations("h", "w", old, released...) {
				t.Fatal("stale receipt accepted")
			}
			if p.routes["new-token"].holds["old-install"] {
				t.Fatal("confirmed stale install retained its reservation")
			}
			req := httptest.NewRequest(http.MethodPost, "http://proxy/v1/chat/completions", nil)
			req.Header.Set("Authorization", "Bearer new-token")
			res := httptest.NewRecorder()
			p.ServeHTTP(res, req)
			if res.Code != 204 {
				t.Fatalf("live runtime route revoked: %d %s", res.Code, res.Body)
			}
		})
	}
}

func TestRemoteIncarnationReclaimsOldReservationsAndRejectsLateBuilders(t *testing.T) {
	scope := credentialProxyScope("h", "w")
	p := &credentialProxy{routes: map[string]*credProxyRoute{"token": {scope: scope, revision: "old", holds: map[string]bool{"old-offer": true}}}}
	app := &App{credProxy: p}
	status := remoteModelSettingsStatus{Version: 1, ModelSettingsOwnership: config.ModelSettingsOwnership{OwnershipIncarnation: "old", OwnershipSeq: 1}}
	app.pinCredentialProxyOwnership("h", "w", status)
	app.reserveCredentialProxyInstall("h", "w", "old-offer", "old", "old")
	status.OwnershipIncarnation = "new"
	app.pinCredentialProxyOwnership("h", "w", status)
	if len(p.routes["token"].holds) != 0 {
		t.Fatal("old incarnation leaked reservations")
	}
	if app.reserveCredentialProxyInstall("h", "w", "late-offer", "old", "old") {
		t.Fatal("late old builder registered a reservation")
	}
	app.reconcileCredentialProxyGenerations("h", "w", status)
	if p.routes["token"] != nil {
		t.Fatal("dead process route was not retired")
	}
}

func TestRemoteReplacedConnectionCannotPinOwnership(t *testing.T) {
	p := &credentialProxy{routes: map[string]*credProxyRoute{}}
	app := &App{credProxy: p}
	old, current := &managedHost{}, &managedHost{}
	m := &desktopRemoteManager{hosts: map[string]*managedHost{"h": old}}
	status := remoteModelSettingsStatus{Version: 1, ModelSettingsOwnership: config.ModelSettingsOwnership{OwnershipIncarnation: "new", OwnershipSeq: 1}}
	// The old operation has already captured its managed pointer and GET. A
	// replacement publishes and pins before that old operation resumes.
	m.mu.Lock()
	m.hosts["h"] = current
	m.mu.Unlock()
	if !m.pinModelSettingsOwnership(app, "h", "w", current, status) {
		t.Fatal("current pin failed")
	}
	status.OwnershipIncarnation = "old"
	if m.pinModelSettingsOwnership(app, "h", "w", old, status) {
		t.Fatal("replaced connection regained authority")
	}
	if p.ownership[credentialProxyScope("h", "w")].OwnershipIncarnation != "new" {
		t.Fatal("incarnation rolled back")
	}
}

func TestRemoteUnknownInstallRetainsOfferUntilOwned(t *testing.T) {
	scope := credentialProxyScope("h", "w")
	p := &credentialProxy{routes: map[string]*credProxyRoute{"token": {scope: scope, revision: "candidate", holds: map[string]bool{"offer": true}}}}
	app := &App{credProxy: p}
	status := remoteModelSettingsStatus{Version: 1, ModelSettingsOwnership: config.ModelSettingsOwnership{OwnershipIncarnation: "serve", OwnershipSeq: 1}, OwnedRevisions: []string{"old"}}
	app.pinCredentialProxyOwnership("h", "w", status)
	app.reserveCredentialProxyInstall("h", "w", "offer", "candidate", "serve")
	app.reconcileCredentialProxyGenerations("h", "w", status)
	if p.routes["token"] == nil || !p.routes["token"].holds["offer"] {
		t.Fatal("uncertain installation lost reservation")
	}
	status.OwnershipSeq++
	status.OwnedRevisions = []string{"candidate"}
	app.reconcileCredentialProxyGenerations("h", "w", status)
	if p.routes["token"] == nil || len(p.routes["token"].holds) != 0 {
		t.Fatal("owned installation did not release reservation")
	}
	status.OwnershipSeq++
	status.OwnedRevisions = []string{"next"}
	app.reconcileCredentialProxyGenerations("h", "w", status)
	if p.routes["token"] != nil {
		t.Fatal("superseded installation route leaked")
	}
}

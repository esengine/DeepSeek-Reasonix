package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"reasonix/internal/config"
)

func TestRemoteInstallDistinguishesRejectionFromLostAcknowledgement(t *testing.T) {
	for _, uncertain := range []bool{false, true} {
		name := "rejected"
		if uncertain {
			name = "published but responses lost"
		}
		t.Run(name, func(t *testing.T) {
			status := remoteModelSettingsStatus{Version: 1, Model: "p/old", Revision: "old", ModelSettingsOwnership: config.ModelSettingsOwnership{OwnershipIncarnation: "serve", OwnershipSeq: 1}, OwnedRevisions: []string{"old"}}
			var recoverResponse atomic.Bool
			var published atomic.Bool
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if uncertain && !recoverResponse.Load() {
					if r.Method == http.MethodPost {
						published.Store(true)
					}
					conn, _, err := w.(http.Hijacker).Hijack()
					if err != nil {
						t.Error(err)
						return
					}
					conn.Close()
					return
				}
				if r.Method == http.MethodPost {
					http.Error(w, "candidate build rejected", http.StatusConflict)
					return
				}
				observed := status
				observed.OwnershipSeq = 3
				if published.Load() {
					observed.Revision, observed.Model, observed.OwnedRevisions = "candidate", "p/new", []string{"candidate"}
				}
				_ = json.NewEncoder(w).Encode(observed)
			}))
			defer server.Close()
			p := &credentialProxy{routes: map[string]*credProxyRoute{"token": {scope: credentialProxyScope("h", "w"), revision: "candidate", holds: map[string]bool{"offer": true}}}}
			app := &App{credProxy: p}
			app.pinCredentialProxyOwnership("h", "w", status)
			bundle := &config.ModelRuntimeSettings{Revision: "candidate", OfferID: "offer"}
			if _, err := app.installRemoteModelSettingsSnapshot(context.Background(), server.Client(), server.URL, "h", "w", "", "p/new", bundle, status); err == nil {
				t.Fatal("expected rejected or uncertain result")
			}
			if !uncertain {
				if p.routes["token"] != nil || len(p.ownership[credentialProxyScope("h", "w")].pending) != 0 {
					t.Fatal("explicit rejection leaked reservation")
				}
				return
			}
			if !published.Load() || p.routes["token"] == nil || !p.routes["token"].holds["offer"] {
				t.Fatal("lost acknowledgements revoked a published candidate")
			}
			recoverResponse.Store(true)
			observed, err := remoteModelSettingsRequest(context.Background(), server.Client(), server.URL, "", nil)
			if err != nil {
				t.Fatal(err)
			}
			app.reconcileCredentialProxyGenerations("h", "w", observed)
			if p.routes["token"] == nil || len(p.routes["token"].holds) != 0 {
				t.Fatal("confirmed published candidate did not settle reservation")
			}
		})
	}
}

package main

import (
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"reasonix/internal/event"
)

// Exercise the real GET capture boundary: a request started before a route
// switch cannot invalidate either the new foreground or its background rows.
func TestRemoteRuntimeSnapshotAcrossRouteSwitch(t *testing.T) {
	for _, outcome := range []string{"missing", "previous-only", "failed"} {
		t.Run(outcome, func(t *testing.T) {
			isolateDesktopUserDirs(t)
			entered, release := make(chan struct{}), make(chan struct{})
			unblock := sync.OnceFunc(func() { close(release) })
			defer unblock()
			first := true
			old := remoteRuntimeTestSnapshot("previous", 3, "executing")
			next := remoteRuntimeTestSnapshot("next", 5, "idle")
			client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				if first {
					first = false
					close(entered)
					<-release
					if outcome == "failed" {
						return nil, errors.New("old request failed")
					}
					if outcome == "previous-only" {
						return remoteRuntimeTestResponse(req, 200, remoteRuntimeTestPayload(t, old)), nil
					}
					return remoteRuntimeTestResponse(req, 200, `{"schemaVersion":1,"sessions":[]}`), nil
				}
				return remoteRuntimeTestResponse(req, 200, remoteRuntimeTestJSON(t, map[string]any{
					"schemaVersion": 1, "sessions": []any{map[string]any{"sessionPath": "session-id:next", "state": next}},
				})), nil
			})}
			a, tab := remoteRuntimeTestApp(client)
			acceptRemoteRuntimeStateLocked(tab, runtimeRemoteTestPath, old, true)
			result := make(chan error, 1)
			go func() { _, err := a.SyncRuntimeState(); result <- err }()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("GET did not start")
			}
			a.remoteTabMu.Lock()
			commitRemoteTabAttachRoute(tab, "session-id:next", true)
			acceptRemoteRuntimeStateLocked(tab, "session-id:next", next, true)
			a.remoteTabMu.Unlock()
			unblock()
			select {
			case <-result:
			case <-time.After(5 * time.Second):
				t.Fatal("GET did not finish")
			}
			views := a.GetRuntimeStateSnapshot().Sessions
			if len(views) != 2 {
				t.Fatalf("stale GET removed background state: %+v", views)
			}
			for _, view := range views {
				if view.Freshness != "synced" {
					t.Fatalf("switch produced a disconnected badge: %+v", view)
				}
			}
			if _, err := a.SyncRuntimeState(); err != nil {
				t.Fatal(err)
			}
			views = a.GetRuntimeStateSnapshot().Sessions
			if len(views) != 1 || views[0].SessionPath != "session-id:next" || views[0].Freshness != "synced" {
				t.Fatalf("fresh GET must retire absent background state without a warning: %+v", views)
			}
		})
	}
}

func TestRemoteRuntimeConnectionFailureAndRecoveryAffectOwnSessions(t *testing.T) {
	isolateDesktopUserDirs(t)
	failed := true
	state := remoteRuntimeTestSnapshot("current", 2, "idle")
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if failed {
			return nil, errors.New("connection lost")
		}
		return remoteRuntimeTestResponse(req, 200, remoteRuntimeTestPayload(t, state)), nil
	})}
	a, tab := remoteRuntimeTestApp(client)
	acceptRemoteRuntimeStateLocked(tab, runtimeRemoteTestPath, state, true)
	acceptRemoteRuntimeStateLocked(tab, "session-id:background", event.RuntimeStateSnapshot{
		SchemaVersion: 1, RuntimeEpoch: "background", Revision: 1, Phase: "idle",
	}, true)
	if _, err := a.SyncRuntimeState(); err == nil {
		t.Fatal("real failure disappeared")
	}
	for _, view := range a.GetRuntimeStateSnapshot().Sessions {
		if view.Freshness != "unknown" {
			t.Fatalf("real disconnection must remain visible: %+v", view)
		}
	}
	failed = false
	if _, err := a.SyncRuntimeState(); err != nil {
		t.Fatal(err)
	}
	views := a.GetRuntimeStateSnapshot().Sessions
	if len(views) != 1 || views[0].Freshness != "synced" {
		t.Fatalf("recovery left a stale warning: %+v", views)
	}
}

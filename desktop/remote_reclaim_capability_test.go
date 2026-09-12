package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRemoteStatusReclaimCapabilityChangesWithoutOwnershipChange(t *testing.T) {
	tab := &remoteTab{id: "remote", state: "ready"}
	for _, tc := range []struct {
		body               string
		takenOver, blocked bool
	}{
		{`{"takenOver":true,"reclaimable":false}`, true, true},
		{`{"takenOver":true,"reclaimable":true}`, true, false},
		{`{"takenOver":true,"reclaimable":false}`, true, true},
		{`{"takenOver":false}`, false, false},
		// Old Serve builds lack reclaimable; preserve their existing action.
		{`{"takenOver":true}`, true, false},
	} {
		var payload remoteTabStatusPayload
		if err := json.Unmarshal([]byte(tc.body), &payload); err != nil {
			t.Fatal(err)
		}
		applyRemoteTabStatusPayload(tab, payload)
		meta := remoteTabMetaLocked(tab)
		if meta.TakenOver != tc.takenOver || meta.ReclaimBlocked != tc.blocked {
			t.Fatalf("status %s: takenOver=%v blocked=%v", tc.body, meta.TakenOver, meta.ReclaimBlocked)
		}
	}
}

func TestReclaimSuccessSurvivesConcurrentStatusPoll(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	requestBody := make(chan []byte, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/reclaim" {
			body, _ := io.ReadAll(r.Body)
			requestBody <- body
			close(started)
			<-release
			w.WriteHeader(http.StatusNoContent)
			return
		}
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	app := NewApp()
	events := &eventLog{}
	app.remoteEventHook = events.add
	tab := &remoteTab{id: "remote", state: "ready", gen: 1, client: srv.Client(), base: srv.URL,
		routing: remoteTabSessionRouting{currentPath: "/session.jsonl"},
		session: remoteTabSessionState{takenOver: true}}
	app.remoteTabs = map[string]*remoteTab{tab.id: tab}
	done := make(chan error, 1)
	go func() { done <- app.ReclaimRemoteTabSession(tab.id) }()
	<-started
	app.remoteTabMu.Lock()
	tab.runtime.revision++ // A status poll is not a new session selection.
	app.remoteTabMu.Unlock()
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	var reclaim struct {
		Mode  string `json:"mode"`
		Force bool   `json:"force"`
	}
	if err := json.Unmarshal(<-requestBody, &reclaim); err != nil || reclaim.Mode != "interrupt" || !reclaim.Force {
		t.Fatalf("reclaim request = %+v, err=%v", reclaim, err)
	}
	app.remoteTabTasks.Wait()
	app.remoteTabMu.Lock()
	defer app.remoteTabMu.Unlock()
	if tab.session.takenOver {
		t.Fatal("status poll discarded successful reclaim")
	}
	if events.count("remote-tab:remote:state ") != 1 {
		t.Fatalf("successful reclaim did not republish ready: %v", events.recorded())
	}
}

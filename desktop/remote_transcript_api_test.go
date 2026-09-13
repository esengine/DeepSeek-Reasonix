package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/transcript"
)

func TestRemoteTranscriptReadsUnsavedLegacySession(t *testing.T) {
	for _, tc := range []struct {
		name      string
		identity  string
		path      string
		wantError bool
	}{
		{"matching new session", "session", "/session.jsonl", false},
		{"different projection", "other", "/session.jsonl", true},
		{"same name different directory", "session", "/other/session.jsonl", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/status" {
					_ = json.NewEncoder(w).Encode(map[string]string{"sessionPath": tc.path})
					return
				}
				if r.URL.Query().Get("session") != "" {
					http.Error(w, "transcript session is not bound to this runtime", http.StatusConflict)
					return
				}
				_ = json.NewEncoder(w).Encode(transcript.Snapshot{Boundary: transcript.Boundary{
					ProtocolVersion: transcript.ProtocolVersion, Identity: transcript.Identity{SessionID: tc.identity},
				}})
			}))
			defer server.Close()
			app, tab := remoteTranscriptFixture(server)
			result, err := app.RemoteTranscriptSnapshotForTab(tab.id, transcript.PageRequest{})
			if (err != nil) != tc.wantError {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			if !tc.wantError && (!result.Supported || result.Snapshot.Identity.SessionID != agent.BranchID(tab.routing.currentPath)) {
				t.Fatalf("result=%+v", result)
			}
		})
	}
}

func TestRemoteTranscriptRejectsRotationDuringLegacyRetry(t *testing.T) {
	app := &App{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/status" {
			_ = json.NewEncoder(w).Encode(map[string]string{"sessionPath": "/session.jsonl"})
			return
		}
		if r.URL.Query().Get("session") != "" {
			http.Error(w, "transcript session is not bound to this runtime", http.StatusConflict)
			return
		}
		app.remoteTabMu.Lock()
		app.remoteTabs["remote"].routing.currentPath = "/new.jsonl"
		app.remoteTabMu.Unlock()
		_ = json.NewEncoder(w).Encode(transcript.Snapshot{Boundary: transcript.Boundary{ProtocolVersion: transcript.ProtocolVersion, Identity: transcript.Identity{SessionID: "session"}}})
	}))
	defer server.Close()
	fixture, tab := remoteTranscriptFixture(server)
	app.remoteTabs = fixture.remoteTabs
	if _, err := app.RemoteTranscriptSnapshotForTab(tab.id, transcript.PageRequest{}); err == nil || !strings.Contains(err.Error(), "replaced session") {
		t.Fatalf("late result accepted: %v", err)
	}
}

func TestRemoteTranscriptContentNeverRetriesWithoutSession(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Query().Get("session") != "/session.jsonl" {
			t.Error("content lost its session binding")
		}
		http.Error(w, "transcript session is not bound to this runtime", http.StatusConflict)
	}))
	defer server.Close()
	app, tab := remoteTranscriptFixture(server)
	if _, err := app.RemoteTranscriptContentForTab(tab.id, transcript.ContentRequest{}); err == nil || calls.Load() != 1 {
		t.Fatalf("calls=%d err=%v", calls.Load(), err)
	}
}

func remoteTranscriptFixture(server *httptest.Server) (*App, *remoteTab) {
	tab := &remoteTab{id: "remote", state: "ready", client: server.Client(), base: server.URL, gen: 1,
		routing: remoteTabSessionRouting{currentPath: "/session.jsonl"}}
	return &App{remoteTabs: map[string]*remoteTab{tab.id: tab}}, tab
}

func TestRemoteTranscriptNegotiatesOldServeWithoutMutation(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusNotImplemented, http.StatusOK} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/transcript/snapshot" || r.URL.Query().Get("session") != "/session.jsonl" {
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
				}
				w.WriteHeader(status)
				_, _ = w.Write([]byte("<html>old Serve index</html>"))
			}))
			defer server.Close()
			app, tab := remoteTranscriptFixture(server)
			result, err := app.RemoteTranscriptSnapshotForTab(tab.id, transcript.PageRequest{})
			if err != nil || result.Supported || result.Snapshot != nil {
				t.Fatalf("negotiation = %+v, %v", result, err)
			}
			if tab.state != "ready" || tab.gen != 1 {
				t.Fatal("capability probe changed the connection")
			}
		})
	}
}

func TestRemoteTranscriptNegotiatesProjectionRestoreFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/transcript/snapshot" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		http.Error(w, "transcript projection is unavailable\nsession event log uses a newer schema", http.StatusConflict)
	}))
	defer server.Close()
	app, tab := remoteTranscriptFixture(server)
	result, err := app.RemoteTranscriptSnapshotForTab(tab.id, transcript.PageRequest{})
	if err != nil || result.Supported || result.Snapshot != nil {
		t.Fatalf("negotiation = %+v, %v", result, err)
	}
}

func TestRemoteTranscriptKeepsSessionConflictsFatal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "transcript session is not bound to this runtime", http.StatusConflict)
	}))
	defer server.Close()
	app, tab := remoteTranscriptFixture(server)
	if _, err := app.RemoteTranscriptSnapshotForTab(tab.id, transcript.PageRequest{}); err == nil {
		t.Fatal("session mismatch was negotiated as an old Serve")
	}
}

func TestRemoteTranscriptRejectsLateSessionResponse(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-release
		_ = json.NewEncoder(w).Encode(transcript.Snapshot{Boundary: transcript.Boundary{ProtocolVersion: 1, SnapshotID: "old"}})
	}))
	defer server.Close()
	app, tab := remoteTranscriptFixture(server)
	done := make(chan error, 1)
	go func() { _, err := app.RemoteTranscriptSnapshotForTab(tab.id, transcript.PageRequest{}); done <- err }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("request did not start")
	}
	app.remoteTabMu.Lock()
	tab.routing.currentPath = "/new-session.jsonl"
	app.remoteTabMu.Unlock()
	close(release)
	if err := <-done; err == nil || !strings.Contains(err.Error(), "replaced session") {
		t.Fatalf("late response accepted: %v", err)
	}
}

func TestRemoteTabMetadataDoesNotRequestHistory(t *testing.T) {
	var historyReads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/history" {
			historyReads.Add(1)
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/status" {
			_, _ = w.Write([]byte(`{"sessionPath":"/session.jsonl","running":false}`))
			return
		}
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()
	app, tab := remoteTranscriptFixture(server)
	metadata, err := app.RemoteTabMetadata(tab.id)
	if err != nil || historyReads.Load() != 0 || len(metadata.History) != 0 {
		t.Fatalf("metadata requested history: count=%d error=%v", historyReads.Load(), err)
	}
}

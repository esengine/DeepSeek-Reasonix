package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/serve"
	"reasonix/internal/sessioninbox"
)

type remoteInboxRunner struct {
	started chan string
	release chan struct{}
}

func (r remoteInboxRunner) Run(ctx context.Context, input string) error {
	r.started <- input
	select {
	case <-r.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type remoteInboxSink struct {
	states chan event.RuntimeStateSnapshot
}

func (*remoteInboxSink) Emit(event.Event) {}
func (s *remoteInboxSink) RuntimeStateChanged(state event.RuntimeStateSnapshot) {
	s.states <- state
}

func TestRemoteRuntimeInboxReceiptSnapshotAndConsumptionOverHTTP(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "remote.jsonl")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	runner := remoteInboxRunner{started: make(chan string, 2), release: make(chan struct{}, 2)}
	sink := &remoteInboxSink{states: make(chan event.RuntimeStateSnapshot, 64)}
	ctrl := control.New(control.Options{SessionDir: dir, SessionPath: path, Runner: runner, Sink: sink})
	t.Cleanup(func() {
		defer ctrl.Close()
		before := ctrl.RuntimeStateSnapshot()
		if before.Phase != "executing" && before.Phase != "finishing" {
			return
		}
		_ = ctrl.SetInboxPaused(true)
		ctrl.Cancel()
		deadline := time.NewTimer(5 * time.Second)
		defer deadline.Stop()
		for {
			select {
			case state := <-sink.states:
				if state.Phase == "idle" && state.Revision > before.Revision {
					return
				}
			case <-deadline.C:
				t.Error("isolated inbox runner did not settle before cleanup")
				return
			}
		}
	})
	server := httptest.NewServer(serve.New(ctrl, nil, config.ServeConfig{}).Handler())
	defer server.Close()
	a, tab := remoteRuntimeTestApp(server.Client())
	tab.base, tab.routing.currentPath, tab.session.path = server.URL, path, path
	ctrl.Send("hold first turn")
	select {
	case <-runner.started:
	case <-time.After(5 * time.Second):
		t.Fatal("first turn did not start")
	}
	receipt, err := a.EnqueueInboxFollowupWithInvocations(tab.id, "rich follow-up", "next input", nil, "remote-inbox-key")
	if err != nil || receipt.ItemID == "" {
		t.Fatalf("enqueue = %+v, %v", receipt, err)
	}
	snapshot, err := a.InboxSnapshot(tab.id)
	if err != nil || snapshot.SessionPath != path || len(snapshot.Items) != 1 || snapshot.Items[0].ID != receipt.ItemID || snapshot.Items[0].Preview != "rich follow-up" || snapshot.MaxItems == 0 || snapshot.Bytes == 0 {
		t.Fatalf("receipt did not resolve to durable composer snapshot: %+v, %v", snapshot, err)
	}
	runner.release <- struct{}{}
	select {
	case <-runner.started:
	case <-time.After(5 * time.Second):
		t.Fatal("durable follow-up was not dispatched")
	}
	active, err := a.InboxSnapshot(tab.id)
	if err != nil || len(active.Items) != 1 || active.Items[0].ID != receipt.ItemID || active.Items[0].State != "running" || active.Revision <= snapshot.Revision || !ctrl.RuntimeStateSnapshot().Running {
		t.Fatalf("dispatched follow-up did not publish its composer-hidden state while still running: %+v, %v", active, err)
	}
	runner.release <- struct{}{}
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case state := <-sink.states:
			if state.Phase != "idle" {
				continue
			}
			consumed, err := a.InboxSnapshot(tab.id)
			if err != nil || consumed.Items == nil || len(consumed.Items) != 0 || consumed.Revision <= snapshot.Revision {
				t.Fatalf("consumption did not remove durable item: %+v, %v", consumed, err)
			}
			return
		case <-deadline.C:
			t.Fatal("follow-up did not settle")
		}
	}
}

func TestRemoteRuntimeInboxRejectsStaleResponseIdentity(t *testing.T) {
	for _, change := range []string{"generation", "selection", "client", "base", "path", "rehydrating", "replaced", "closed"} {
		t.Run(change, func(t *testing.T) {
			started, release := make(chan struct{}), make(chan struct{})
			client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				if req.URL.Path != "/inbox" || req.URL.Query().Get("session") != runtimeRemoteTestPath {
					t.Errorf("unfenced inbox request: %s", req.URL)
				}
				close(started)
				<-release
				return remoteRuntimeTestResponse(req, http.StatusOK, `{"sessionPath":"`+runtimeRemoteTestPath+`","revision":4,"items":[]}`), nil
			})}
			a, tab := remoteRuntimeTestApp(client)
			done := make(chan error, 1)
			go func() { _, err := a.InboxSnapshot(tab.id); done <- err }()
			<-started
			a.remoteTabMu.Lock()
			switch change {
			case "generation":
				tab.gen++
			case "selection":
				tab.selectionRevision++
			case "client":
				tab.client = &http.Client{}
			case "base":
				tab.base = "http://other.invalid"
			case "path":
				tab.routing.currentPath = "/other.jsonl"
			case "rehydrating":
				tab.routing.rehydratingPath = runtimeRemoteTestPath
			case "replaced":
				a.remoteTabs[tab.id] = &remoteTab{id: tab.id}
			case "closed":
				delete(a.remoteTabs, tab.id)
			}
			a.remoteTabMu.Unlock()
			close(release)
			if err := <-done; err == nil {
				t.Fatal("stale remote inbox response was accepted")
			}
		})
	}
}

func TestRemoteRuntimeInboxRejectsMissingOrWrongResponseSession(t *testing.T) {
	for _, path := range []string{"", "/sessions/other.jsonl"} {
		t.Run(path, func(t *testing.T) {
			data, _ := json.Marshal(sessioninbox.InboxSnapshot{SessionPath: path})
			a, tab := remoteRuntimeTestApp(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return remoteRuntimeTestResponse(req, http.StatusOK, string(data)), nil
			})})
			if _, err := a.InboxSnapshot(tab.id); err == nil {
				t.Fatal("unscoped remote inbox response was accepted")
			}
		})
	}
}

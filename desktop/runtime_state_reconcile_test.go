package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"reasonix/internal/agent"
)

func TestRemoteRuntimeMissingSelectedSessionRequiresOwnConfirmation(t *testing.T) {
	isolateDesktopUserDirs(t)
	body := `{"schemaVersion":1,"sessions":[]}`
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return remoteRuntimeTestResponse(req, http.StatusOK, body), nil
	})}
	a, tab := remoteRuntimeTestApp(client)
	old := remoteRuntimeTestSnapshot("retired", 7, "executing")
	acceptRemoteRuntimeStateLocked(tab, runtimeRemoteTestPath, old, true)
	if _, err := a.SyncRuntimeState(); err != nil {
		t.Fatal(err)
	}
	view := a.GetRuntimeStateSnapshot().Sessions[0]
	if view.Freshness != "unknown" || view.State != old {
		t.Fatalf("missing runtime became authoritative: %+v", view)
	}
	other := remoteRuntimeTestSnapshot("other", 1, "idle")
	acceptRemoteRuntimeStateLocked(tab, "/sessions/other.jsonl", other, true)
	if tab.runtimeUnknown[runtimeRemoteTestPath] == 0 {
		t.Fatal("another session cleared uncertainty")
	}
	body = remoteRuntimeTestPayload(t, old)
	if _, err := a.SyncRuntimeState(); err != nil {
		t.Fatal(err)
	}
	view = a.GetRuntimeStateSnapshot().Sessions[0]
	if view.Freshness != "synced" || view.State != old {
		t.Fatalf("identical authority did not confirm: %+v", view)
	}
}

func TestRemoteRuntimeGETCannotConfirmLaterUncertainty(t *testing.T) {
	isolateDesktopUserDirs(t)
	entered, release := make(chan struct{}), make(chan struct{})
	enterOnce, releaseOnce := sync.OnceFunc(func() { close(entered) }), sync.OnceFunc(func() { close(release) })
	defer releaseOnce()
	state := remoteRuntimeTestSnapshot("controller", 3, "executing")
	body := remoteRuntimeTestPayload(t, state)
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		enterOnce()
		<-release
		return remoteRuntimeTestResponse(req, 200, body), nil
	})}
	a, tab := remoteRuntimeTestApp(client)
	acceptRemoteRuntimeStateLocked(tab, runtimeRemoteTestPath, state, true)
	result := make(chan error, 1)
	go func() { _, err := a.SyncRuntimeState(); result <- err }()
	<-entered
	a.remoteTabMu.Lock()
	markRemoteRuntimeUnknownLocked(tab, runtimeRemoteTestPath)
	a.remoteTabMu.Unlock()
	releaseOnce()
	awaitRemoteRuntimeSync(t, result)
	if tab.runtimeUnknown[runtimeRemoteTestPath] == 0 {
		t.Fatal("old GET erased a later failure without changed facts")
	}
	if _, err := a.SyncRuntimeState(); err != nil {
		t.Fatal(err)
	}
	if tab.runtimeUnknown[runtimeRemoteTestPath] != 0 {
		t.Fatal("fresh GET did not restore confidence")
	}
}

func TestRemoteRuntimeLegacyRecoveryConfirmsOnlySelectedSession(t *testing.T) {
	isolateDesktopUserDirs(t)
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == "/runtime-states" {
			return remoteRuntimeTestResponse(req, 404, "unsupported"), nil
		}
		return remoteRuntimeTestResponse(req, 200, `{"sessionPath":"/sessions/current.jsonl","running":false}`), nil
	})}
	a, tab := remoteRuntimeTestApp(client)
	state := remoteRuntimeTestSnapshot("previous-server", 1, "executing")
	acceptRemoteRuntimeStateLocked(tab, runtimeRemoteTestPath, state, true)
	acceptRemoteRuntimeStateLocked(tab, "/sessions/background.jsonl", state, true)
	markRemoteRuntimeUnknownLocked(tab, runtimeRemoteTestPath)
	markRemoteRuntimeUnknownLocked(tab, "/sessions/background.jsonl")
	if _, err := a.SyncRuntimeState(); err != nil {
		t.Fatal(err)
	}
	for _, session := range a.GetRuntimeStateSnapshot().Sessions {
		if session.Open {
			if session.State.SchemaVersion != 0 || session.State.Running || session.Freshness != "synced" {
				t.Fatalf("legacy recovery = %+v", session)
			}
		} else if session.Freshness != "unknown" {
			t.Fatal("legacy foreground status confirmed background")
		}
	}
}

func TestRuntimeProjectionDoesNotReadPreviewFiles(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := t.TempDir()
	first, second := filepath.Join(dir, "first.jsonl"), filepath.Join(dir, "second.jsonl")
	a := &App{tabs: map[string]*WorkspaceTab{
		"one": {ID: "one", Scope: "project", WorkspaceRoot: dir, TopicID: "topic", SessionPath: first},
		"two": {ID: "two", Scope: "project", WorkspaceRoot: dir, TopicID: "topic", SessionPath: second},
	}}
	if err := os.WriteFile(agent.BranchMetaPath(first), []byte(`{"preview":"before"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	before := a.GetRuntimeStateSnapshot()
	if err := os.WriteFile(agent.BranchMetaPath(first), []byte(`{"preview":"after"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	for range 20 {
		if after := a.GetRuntimeStateSnapshot(); !reflect.DeepEqual(before, after) {
			t.Fatalf("unstable memory projection: %+v", after)
		}
	}
}

func TestRemoteRuntimeWireRequiredFacts(t *testing.T) {
	valid := remoteRuntimeTestJSON(t, remoteRuntimeTestSnapshot("controller", 2, "idle"))
	if state, err := decodeRemoteRuntimeState(json.RawMessage(valid)); err != nil || state.Running || state.BackgroundJobs != 0 {
		t.Fatalf("explicit false/zero rejected: %+v %v", state, err)
	}
	for _, field := range []string{"schemaVersion", "runtimeEpoch", "revision", "phase", "running", "pendingPrompt", "cancelRequested", "cancellable", "backgroundJobs"} {
		for _, replacement := range []string{"missing", "null", `[]`} {
			t.Run(field+"/"+replacement, func(t *testing.T) {
				var fields map[string]json.RawMessage
				if err := json.Unmarshal([]byte(valid), &fields); err != nil {
					t.Fatal(err)
				}
				if replacement == "missing" {
					delete(fields, field)
				} else {
					fields[field] = json.RawMessage(replacement)
				}
				raw, err := json.Marshal(fields)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := decodeRemoteRuntimeState(raw); err == nil {
					t.Fatal("accepted absent or malformed required fact")
				}
				var status remoteTabStatusPayload
				if err := json.Unmarshal([]byte(`{"running":false,"runtimeState":`+string(raw)+`}`), &status); err == nil {
					t.Fatal("status downgraded malformed schema 1 to legacy flags")
				}
			})
		}
	}
}

func TestRemoteRuntimeInvalidFullSnapshotCannotPrune(t *testing.T) {
	isolateDesktopUserDirs(t)
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return remoteRuntimeTestResponse(req, 200, `{"schemaVersion":1,"sessions":[{"sessionPath":"/sessions/current.jsonl","state":{"schemaVersion":1,"runtimeEpoch":"controller","revision":2,"phase":"idle"}}]}`), nil
	})}
	a, tab := remoteRuntimeTestApp(client)
	old := remoteRuntimeTestSnapshot("controller", 1, "executing")
	acceptRemoteRuntimeStateLocked(tab, runtimeRemoteTestPath, old, true)
	acceptRemoteRuntimeStateLocked(tab, "/sessions/background.jsonl", old, true)
	if _, err := a.SyncRuntimeState(); err == nil {
		t.Fatal("invalid payload accepted")
	}
	if len(tab.runtimeStates) != 2 || tab.runtimeStates[runtimeRemoteTestPath] != old {
		t.Fatal("invalid full GET replaced or pruned facts")
	}
	for _, view := range a.GetRuntimeStateSnapshot().Sessions {
		if view.Freshness != "unknown" {
			t.Fatal("invalid full GET cleared uncertainty")
		}
	}
	a.acceptRemoteRuntimeFrame(tab.id, tab.gen, runtimeRemoteTestPath, json.RawMessage(`{"runtimeState":{"schemaVersion":1,"runtimeEpoch":"controller","revision":2,"phase":"idle"}}`))
	if tab.runtimeStates[runtimeRemoteTestPath] != old {
		t.Fatal("invalid SSE replaced facts")
	}
}

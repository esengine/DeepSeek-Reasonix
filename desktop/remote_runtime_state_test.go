package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/event"
)

const runtimeRemoteTestPath = "/sessions/current.jsonl"

func remoteRuntimeTestSnapshot(epoch string, revision uint64, phase string) event.RuntimeStateSnapshot {
	return event.RuntimeStateSnapshot{SchemaVersion: 1, RuntimeEpoch: epoch, Revision: revision, Phase: phase,
		Running: phase == "executing" || phase == "finishing", Cancellable: phase == "executing"}
}

func remoteRuntimeTestApp(client *http.Client) (*App, *remoteTab) {
	tab := &remoteTab{id: "remote-runtime", state: "ready", gen: 7, selectionRevision: 3,
		client: client, base: "http://runtime-fixture.invalid", ref: RemoteTabRef{HostID: "fixture-host", Workspace: "/workspace"},
		session: remoteTabSessionState{name: "current", path: runtimeRemoteTestPath},
		routing: remoteTabSessionRouting{currentPath: runtimeRemoteTestPath, running: map[string]bool{}},
	}
	return &App{remoteTabs: map[string]*remoteTab{tab.id: tab}}, tab
}

func remoteRuntimeTestResponse(req *http.Request, code int, body string) *http.Response {
	return &http.Response{StatusCode: code, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}
}

func remoteRuntimeTestJSON(t *testing.T, value any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func remoteRuntimeTestPayload(t *testing.T, state event.RuntimeStateSnapshot) string {
	return remoteRuntimeTestJSON(t, map[string]any{"schemaVersion": 1, "sessions": []any{map[string]any{"sessionPath": runtimeRemoteTestPath, "state": state}}})
}

func awaitRemoteRuntimeSync(t *testing.T, result <-chan error) {
	t.Helper()
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runtime synchronization did not complete")
	}
}

func TestRemoteRuntimeStateReducerOrdersAndFencesInstances(t *testing.T) {
	_, tab := remoteRuntimeTestApp(nil)
	initial := remoteRuntimeTestSnapshot("epoch-a", 3, "executing")
	if !acceptRemoteRuntimeStateLocked(tab, runtimeRemoteTestPath, initial, true) {
		t.Fatal("authoritative binding rejected")
	}
	hostRevision := tab.runtime.revision
	for _, state := range []event.RuntimeStateSnapshot{
		initial, remoteRuntimeTestSnapshot("epoch-a", 2, "idle"),
		remoteRuntimeTestSnapshot("epoch-a", 3, "idle"), remoteRuntimeTestSnapshot("epoch-b", 9, "idle"),
	} {
		if acceptRemoteRuntimeStateLocked(tab, runtimeRemoteTestPath, state, false) {
			t.Fatalf("accepted duplicate/stale/conflicting/unbound state: %+v", state)
		}
		if tab.runtime.snapshot != initial || tab.runtime.revision != hostRevision {
			t.Fatalf("rejected state mutated projection: %+v", tab.runtime)
		}
	}
	newer := remoteRuntimeTestSnapshot("epoch-a", 4, "idle")
	if !acceptRemoteRuntimeStateLocked(tab, runtimeRemoteTestPath, newer, false) || tab.runtime.running {
		t.Fatal("newer idle did not clear running")
	}
	background := remoteRuntimeTestSnapshot("background", 1, "executing")
	if !acceptRemoteRuntimeStateLocked(tab, "/sessions/background.jsonl", background, true) {
		t.Fatal("background instance registration failed")
	}
	if tab.runtime.snapshot != newer || !tab.routing.running["/sessions/background.jsonl"] {
		t.Fatal("background runtime replaced selected session or failed to aggregate")
	}
	replacement := remoteRuntimeTestSnapshot("epoch-b", 1, "idle")
	if !acceptRemoteRuntimeStateLocked(tab, runtimeRemoteTestPath, replacement, true) || tab.runtime.snapshot != replacement {
		t.Fatal("authoritative new epoch was rejected")
	}
}

func TestRemoteRuntimeStateGETCannotOverwriteNewerSSE(t *testing.T) {
	for _, epochChanged := range []bool{false, true} {
		name := "same-epoch"
		if epochChanged {
			name = "new-epoch"
		}
		t.Run(name, func(t *testing.T) {
			isolateDesktopUserDirs(t)
			entered, release := make(chan struct{}), make(chan struct{})
			releaseGET := sync.OnceFunc(func() { close(release) })
			defer releaseGET()
			response := remoteRuntimeTestSnapshot("epoch-a", 4, "finishing")
			if epochChanged {
				response = remoteRuntimeTestSnapshot("epoch-b", 1, "idle")
			}
			body := remoteRuntimeTestPayload(t, response)
			client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				close(entered)
				<-release
				return remoteRuntimeTestResponse(req, 200, body), nil
			})}
			a, tab := remoteRuntimeTestApp(client)
			acceptRemoteRuntimeStateLocked(tab, runtimeRemoteTestPath, remoteRuntimeTestSnapshot("epoch-a", 2, "executing"), true)
			result := make(chan error, 1)
			go func() { _, err := a.SyncRuntimeState(); result <- err }()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("GET did not start")
			}
			newer := remoteRuntimeTestSnapshot("epoch-a", 5, "idle")
			frame := json.RawMessage(remoteRuntimeTestJSON(t, map[string]any{"runtimeState": newer}))
			a.acceptRemoteRuntimeFrame(tab.id, tab.gen, runtimeRemoteTestPath, frame)
			releaseGET()
			awaitRemoteRuntimeSync(t, result)
			if got := tab.runtime.snapshot; got != newer {
				t.Fatalf("late GET overwrote newer SSE: got=%+v want=%+v", got, newer)
			}
		})
	}
}

func TestRemoteRuntimeStateGETRejectsChangedSelectionAndGeneration(t *testing.T) {
	for _, fence := range []string{"generation", "selection", "client", "rehydration"} {
		t.Run(fence, func(t *testing.T) {
			isolateDesktopUserDirs(t)
			entered, release := make(chan struct{}), make(chan struct{})
			releaseGET := sync.OnceFunc(func() { close(release) })
			defer releaseGET()
			body := remoteRuntimeTestPayload(t, remoteRuntimeTestSnapshot("replacement", 9, "executing"))
			client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				close(entered)
				<-release
				return remoteRuntimeTestResponse(req, 200, body), nil
			})}
			a, tab := remoteRuntimeTestApp(client)
			initial := remoteRuntimeTestSnapshot("initial", 1, "idle")
			acceptRemoteRuntimeStateLocked(tab, runtimeRemoteTestPath, initial, true)
			result := make(chan error, 1)
			go func() { _, err := a.SyncRuntimeState(); result <- err }()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("GET did not start")
			}
			a.remoteTabMu.Lock()
			switch fence {
			case "generation":
				tab.gen++
			case "selection":
				tab.selectionRevision++
			case "client":
				tab.client = &http.Client{}
			case "rehydration":
				tab.routing.rehydratingPath = "/sessions/next.jsonl"
			}
			a.remoteTabMu.Unlock()
			releaseGET()
			awaitRemoteRuntimeSync(t, result)
			if tab.runtime.snapshot != initial {
				t.Fatalf("stale %s request changed runtime: %+v", fence, tab.runtime.snapshot)
			}
		})
	}
}

func TestRemoteRuntimeStateSSERejectsOldPumpAndDuplicate(t *testing.T) {
	a, tab := remoteRuntimeTestApp(nil)
	initial := remoteRuntimeTestSnapshot("epoch", 2, "executing")
	acceptRemoteRuntimeStateLocked(tab, runtimeRemoteTestPath, initial, true)
	var events atomic.Int32
	a.remoteEventHook = func(string, any) { events.Add(1) }
	for _, fixture := range []struct {
		gen   uint64
		state event.RuntimeStateSnapshot
	}{
		{tab.gen - 1, remoteRuntimeTestSnapshot("epoch", 3, "idle")}, {tab.gen, initial},
	} {
		frame := json.RawMessage(remoteRuntimeTestJSON(t, map[string]any{"runtimeState": fixture.state}))
		a.acceptRemoteRuntimeFrame(tab.id, fixture.gen, runtimeRemoteTestPath, frame)
	}
	if tab.runtime.snapshot != initial || events.Load() != 0 {
		t.Fatalf("old/duplicate frame mutated state or notified: state=%+v events=%d", tab.runtime.snapshot, events.Load())
	}
}

func TestRemoteRuntimeStateLegacy404CachedForConnection(t *testing.T) {
	isolateDesktopUserDirs(t)
	var probes, statuses atomic.Int32
	statusBody := remoteRuntimeTestJSON(t, map[string]any{"sessionPath": runtimeRemoteTestPath, "running": false})
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == "/runtime-states" {
			probes.Add(1)
			return remoteRuntimeTestResponse(req, 404, "not supported"), nil
		}
		if req.URL.Path == "/status" {
			statuses.Add(1)
			return remoteRuntimeTestResponse(req, 200, statusBody), nil
		}
		return nil, errors.New("unexpected runtime fallback endpoint")
	})}
	a, _ := remoteRuntimeTestApp(client)
	for range 2 {
		if _, err := a.SyncRuntimeState(); err != nil {
			t.Fatal(err)
		}
	}
	if probes.Load() != 1 || statuses.Load() != 2 {
		t.Fatalf("legacy capability fallback count probes=%d statuses=%d", probes.Load(), statuses.Load())
	}
}

func TestRemoteRuntimeStateReconnectProbesCapabilityAgain(t *testing.T) {
	isolateDesktopUserDirs(t)
	var probes atomic.Int32
	updated := remoteRuntimeTestSnapshot("new-server", 1, "idle")
	body := remoteRuntimeTestPayload(t, updated)
	statusBody := remoteRuntimeTestJSON(t, map[string]any{"sessionPath": runtimeRemoteTestPath, "running": false})
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == "/runtime-states" {
			if probes.Add(1) == 1 {
				return remoteRuntimeTestResponse(req, 404, "old server"), nil
			}
			return remoteRuntimeTestResponse(req, 200, body), nil
		}
		return remoteRuntimeTestResponse(req, 200, statusBody), nil
	})}
	a, tab := remoteRuntimeTestApp(client)
	if _, err := a.SyncRuntimeState(); err != nil {
		t.Fatal(err)
	}
	a.remoteTabMu.Lock()
	tab.gen++
	a.remoteTabMu.Unlock()
	if _, err := a.SyncRuntimeState(); err != nil {
		t.Fatal(err)
	}
	if probes.Load() != 2 || tab.runtime.snapshot != updated {
		t.Fatalf("new connection inherited old capability rejection: probes=%d state=%+v", probes.Load(), tab.runtime.snapshot)
	}
}

func TestRemoteRuntimeFollowupUnknownPOSTLooksUpReceiptWithoutReplay(t *testing.T) {
	isolateDesktopUserDirs(t)
	var posts, lookups atomic.Int32
	var posted map[string]json.RawMessage
	receiptBody := remoteRuntimeTestJSON(t, map[string]any{"itemId": "accepted-item", "disposition": "queued"})
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method == http.MethodPost && req.URL.Path == "/inbox/items" {
			posts.Add(1)
			if err := json.NewDecoder(req.Body).Decode(&posted); err != nil {
				return nil, err
			}
			return nil, errors.New("response lost after durable accept")
		}
		if req.Method == http.MethodGet && req.URL.Path == "/inbox/receipt" {
			lookups.Add(1)
			if req.URL.Query().Get("key") != "stable-key" || req.URL.Query().Get("session") != runtimeRemoteTestPath {
				return nil, errors.New("receipt lookup lost original session/key")
			}
			return remoteRuntimeTestResponse(req, 200, receiptBody), nil
		}
		return nil, errors.New("unexpected followup request")
	})}
	a, tab := remoteRuntimeTestApp(client)
	invocations := []InvocationRequest{{Name: "fixture-skill", Kind: "skill", Offset: 0}}
	receipt, err := a.enqueueRemoteFollowup(tab.id, "rich display", "model input", invocations, "stable-key")
	if err != nil || receipt.ItemID != "accepted-item" || posts.Load() != 1 || lookups.Load() != 1 {
		t.Fatalf("unknown write was lost or replayed: receipt=%+v err=%v posts=%d lookups=%d", receipt, err, posts.Load(), lookups.Load())
	}
	var display, input, key string
	var gotInvocations []InvocationRequest
	_ = json.Unmarshal(posted["display"], &display)
	_ = json.Unmarshal(posted["input"], &input)
	_ = json.Unmarshal(posted["idempotencyKey"], &key)
	_ = json.Unmarshal(posted["invocations"], &gotInvocations)
	if display != "rich display" || input != "model input" || key != "stable-key" || !reflect.DeepEqual(gotInvocations, invocations) {
		t.Fatalf("rich followup changed: %s", remoteRuntimeTestJSON(t, posted))
	}
}

func TestRemoteRuntimeFollowupRejectedPOSTCannotReuseOlderReceipt(t *testing.T) {
	isolateDesktopUserDirs(t)
	var lookups atomic.Int32
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method == http.MethodPost {
			return remoteRuntimeTestResponse(req, http.StatusConflict, "idempotency conflict"), nil
		}
		lookups.Add(1)
		return remoteRuntimeTestResponse(req, http.StatusOK, `{"itemId":"older-request"}`), nil
	})}
	a, tab := remoteRuntimeTestApp(client)
	receipt, err := a.enqueueRemoteFollowup(tab.id, "changed draft", "changed input", nil, "reused-key")
	if err == nil || receipt.ItemID != "" || lookups.Load() != 0 {
		t.Fatalf("definite rejection adopted unrelated receipt: receipt=%+v err=%v lookups=%d", receipt, err, lookups.Load())
	}
}

func TestRemoteRuntimeStateDisconnectPreservesWorkAndReconnectAdoptsEpoch(t *testing.T) {
	isolateDesktopUserDirs(t)
	reconnected := remoteRuntimeTestSnapshot("restarted-server", 1, "idle")
	body := remoteRuntimeTestPayload(t, reconnected)
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return remoteRuntimeTestResponse(req, 200, body), nil
	})}
	a, tab := remoteRuntimeTestApp(client)
	old := remoteRuntimeTestSnapshot("old-server", 8, "executing")
	old.BackgroundJobs = 1
	acceptRemoteRuntimeStateLocked(tab, runtimeRemoteTestPath, old, true)
	oldGen, base := tab.gen, tab.base
	if !a.reconnectRemoteTabGeneration(tab.id, oldGen) {
		t.Fatal("current pump did not enter reconnecting")
	}
	disconnected := a.GetRuntimeStateSnapshot()
	if len(disconnected.Sessions) != 1 || disconnected.Sessions[0].Freshness != "unknown" || disconnected.Sessions[0].State != old {
		t.Fatalf("disconnect fabricated completion or trusted stale work: %+v", disconnected)
	}
	if _, _, _, err := a.remoteTabCommandTarget(tab.id); err == nil {
		t.Fatal("disconnected session still accepts commands")
	}
	// Complete the authenticated connection generation; the state GET may
	// establish the new server epoch while old pump frames remain fenced out.
	a.remoteTabMu.Lock()
	tab.client, tab.base, tab.state = client, base, "ready"
	a.remoteTabMu.Unlock()
	if _, err := a.SyncRuntimeState(); err != nil {
		t.Fatal(err)
	}
	stale := remoteRuntimeTestSnapshot("old-server", 99, "executing")
	frame := json.RawMessage(remoteRuntimeTestJSON(t, map[string]any{"runtimeState": stale}))
	a.acceptRemoteRuntimeFrame(tab.id, oldGen, runtimeRemoteTestPath, frame)
	current := a.GetRuntimeStateSnapshot()
	if current.Sessions[0].State != reconnected || current.Sessions[0].Freshness != "synced" {
		t.Fatalf("reconnect did not converge to new authority: %+v", current)
	}
}

func TestRemoteRuntimeStateOldSelectionFrameOnlyUpdatesBackground(t *testing.T) {
	isolateDesktopUserDirs(t)
	a, tab := remoteRuntimeTestApp(nil)
	old := remoteRuntimeTestSnapshot("old-selection", 2, "executing")
	acceptRemoteRuntimeStateLocked(tab, runtimeRemoteTestPath, old, true)
	nextPath := "/sessions/next.jsonl"
	next := remoteRuntimeTestSnapshot("new-selection", 1, "executing")
	a.remoteTabMu.Lock()
	tab.selectionRevision++
	commitRemoteTabAttachRoute(tab, nextPath, false)
	acceptRemoteRuntimeStateLocked(tab, nextPath, next, true)
	a.remoteTabMu.Unlock()
	oldCompleted := remoteRuntimeTestSnapshot("old-selection", 3, "idle")
	frame := json.RawMessage(remoteRuntimeTestJSON(t, map[string]any{"runtimeState": oldCompleted}))
	a.acceptRemoteRuntimeFrame(tab.id, tab.gen, runtimeRemoteTestPath, frame)
	if tab.routing.currentPath != nextPath || tab.runtime.snapshot != next {
		t.Fatalf("old selection frame changed foreground: path=%q state=%+v", tab.routing.currentPath, tab.runtime.snapshot)
	}
	if tab.runtimeStates[runtimeRemoteTestPath] != oldCompleted || tab.routing.running[runtimeRemoteTestPath] {
		t.Fatal("old selection completion was lost from background aggregation")
	}
}

func TestRemoteRuntimeStatePublicationOrdersGenerationRetirement(t *testing.T) {
	isolateDesktopUserDirs(t)
	a, tab := remoteRuntimeTestApp(&http.Client{})
	initial := remoteRuntimeTestSnapshot("current", 1, "executing")
	acceptRemoteRuntimeStateLocked(tab, runtimeRemoteTestPath, initial, true)
	entered, release := make(chan struct{}), make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	a.remoteEventHook = func(name string, _ any) {
		if name == "remote-tab:updated" {
			close(entered)
			<-release
		}
	}
	finished := make(chan struct{})
	next := remoteRuntimeTestSnapshot("current", 2, "idle")
	frame := json.RawMessage(remoteRuntimeTestJSON(t, map[string]any{"runtimeState": next}))
	go func() { a.acceptRemoteRuntimeFrame(tab.id, 7, runtimeRemoteTestPath, frame); close(finished) }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("runtime frame did not reach metadata publication")
	}
	attempted, retired := make(chan struct{}), make(chan struct{})
	go func() { close(attempted); a.reconnectRemoteTabGeneration(tab.id, 7); close(retired) }()
	<-attempted
	// Match the existing remote publication regression's interleaving: the
	// callback is held at a known boundary, while retirement attempts the same
	// publication fence. This timeout only verifies that it remains blocked.
	select {
	case <-retired:
		t.Fatal("generation retirement overtook in-flight runtime metadata; stale frame can follow the reconnect barrier")
	case <-time.After(30 * time.Millisecond):
	}
	a.remoteTabMu.Lock()
	intact := tab.gen == 7 && tab.state == "ready"
	a.remoteTabMu.Unlock()
	if !intact {
		t.Fatal("generation changed before prior runtime publication completed")
	}
	unblock()
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("runtime publication did not finish")
	}
	select {
	case <-retired:
	case <-time.After(5 * time.Second):
		t.Fatal("generation retirement did not finish")
	}
}

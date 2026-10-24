package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSetActiveTabRepublishesTerminalRemoteState(t *testing.T) {
	log := &eventLog{}
	tab := &remoteTab{
		id: "remote-terminal", ref: RemoteTabRef{HostID: "box", Workspace: "~/app"},
		state: "serve_down", err: "bootstrap failed",
		routing: remoteTabSessionRouting{running: map[string]bool{}},
	}
	a := &App{remoteTabs: map[string]*remoteTab{tab.id: tab}, remoteEventHook: log.add}
	if err := a.SetActiveTab(tab.id); err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, event := range log.recorded() {
		payload, ok := strings.CutPrefix(event, "remote-tab:remote-terminal:state ")
		if !ok {
			continue
		}
		count++
		var state RemoteTabStateView
		if err := json.Unmarshal([]byte(payload), &state); err != nil {
			t.Fatal(err)
		}
		if state.State != "serve_down" || state.Error != "bootstrap failed" || state.HydrationRevision != 1 {
			t.Fatalf("terminal activation state = %+v", state)
		}
	}
	if count != 1 {
		t.Fatalf("terminal activation notifications = %d, want 1", count)
	}
}

func TestRemoteHydrationStateJoinsPublishedBarrier(t *testing.T) {
	tab := &remoteTab{id: "remote-hydration", state: "connecting", gen: 1}
	a := &App{remoteTabs: map[string]*remoteTab{tab.id: tab}}
	var events []RemoteTabStateView
	a.remoteEventHook = func(_ string, value any) {
		if state, ok := value.(RemoteTabStateView); ok {
			events = append(events, state)
		}
	}
	for _, state := range []string{"ready", "ready", "reconnecting", "ready"} {
		if !a.emitRemoteTabStateForGeneration(tab.id, 1, state, "") {
			t.Fatal("publication rejected")
		}
		view, err := a.RemoteTabHydrationState(tab.id)
		if err != nil {
			t.Fatal(err)
		}
		if view.State != state || view.HydrationRevision != uint64(len(events)) || view != events[len(events)-1] {
			t.Fatalf("mount view=%+v event=%+v", view, events[len(events)-1])
		}
	}
	if a.emitRemoteTabStateForGeneration(tab.id, 0, "ready", "") {
		t.Fatal("stale generation published")
	}
	view, err := a.RemoteTabHydrationState(tab.id)
	if err != nil || view.HydrationRevision != 4 {
		t.Fatalf("stale publication changed revision: %+v %v", view, err)
	}
	if _, err := a.RemoteTabHydrationState("absent"); err == nil {
		t.Fatal("missing tab accepted")
	}
	var legacy RemoteTabStateView
	if err := json.Unmarshal([]byte(`{"state":"ready"}`), &legacy); err != nil || legacy.HydrationRevision != 0 {
		t.Fatal("legacy state rejected")
	}
}

func TestRemoteHydrationSnapshotWaitsForPublication(t *testing.T) {
	tab := &remoteTab{id: "remote-publication", state: "connecting", gen: 1}
	a := &App{remoteTabs: map[string]*remoteTab{tab.id: tab}}
	entered, release, published := make(chan struct{}), make(chan struct{}), make(chan struct{})
	a.remoteEventHook = func(_ string, value any) {
		if _, ok := value.(RemoteTabStateView); ok {
			close(entered)
			<-release
		}
	}
	go func() { a.emitRemoteTabStateForGeneration(tab.id, 1, "ready", ""); close(published) }()
	<-entered
	result := make(chan RemoteTabStateView, 1)
	go func() { view, _ := a.RemoteTabHydrationState(tab.id); result <- view }()
	close(release)
	<-published
	if view := <-result; view.State != "ready" || view.HydrationRevision != 1 {
		t.Fatalf("torn publication: %+v", view)
	}
}

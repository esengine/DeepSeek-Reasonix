package main

import (
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRemoteResumeFailurePublicationOrdersRetirement(t *testing.T) {
	for _, kind := range []string{"reconnect", "retire", "suspend", "park", "close", "state"} {
		t.Run(kind, func(t *testing.T) {
			isolateDesktopUserDirs(t)
			client := &http.Client{}
			tab := &remoteTab{id: "remote-1", state: "ready", client: client, gen: 7, selectionRevision: 9,
				ref:     RemoteTabRef{HostID: "box", Workspace: "app"},
				session: remoteTabSessionState{name: "target", path: "/target"}, topicTitle: "Target",
				routing: remoteTabSessionRouting{currentPath: "/target", pathRevision: 11, running: map[string]bool{}},
			}
			a := &App{remoteTabs: map[string]*remoteTab{tab.id: tab}}
			previous := &remoteTabOpenSelection{session: remoteTabSessionState{name: "old", path: "/old"}, topicTitle: "Old", currentPath: "/old", revision: 9}
			route := a.beginRemoteTabProvisionalResume(tab.id, tab, client, 7, "/target")
			route.previousSelection = previous
			entered, release := make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			t.Cleanup(unblock)
			events := &eventLog{}
			a.remoteEventHook = func(name string, payload any) {
				if name == "remote-tab:updated" {
					close(entered)
					<-release
				}
				events.add(name, payload)
			}
			finished := make(chan struct{})
			go func() { a.completeRemoteTabResumeFailure(tab.id, tab, client, 7, route, "rejected"); close(finished) }()
			select {
			case <-entered:
			case <-time.After(3 * time.Second):
				t.Fatal("failure did not reach metadata publication")
			}
			attempted, retired := make(chan struct{}), make(chan struct{})
			go func() {
				close(attempted)
				switch kind {
				case "reconnect":
					a.reconnectRemoteTabGeneration(tab.id, 7)
				case "retire":
					a.retireRemoteTabGeneration(tab.id, 7)
				case "suspend":
					a.suspendRemoteTabPumps("box", "reconnecting", "")
				case "park":
					a.parkRemoteTabsForServer("box", "app", "serve_down", "")
				case "close":
					_ = a.closeRemoteTabRegistration(tab.id, true)
				case "state":
					a.emitRemoteTabStateForGeneration(tab.id, 7, "error", "stream ended")
				}
				close(retired)
			}()
			<-attempted
			select {
			case <-retired:
				t.Fatal("retirement overtook an in-flight failure publication")
			case <-time.After(30 * time.Millisecond):
			}
			a.remoteTabMu.Lock()
			intact := a.remoteTabs[tab.id] == tab && tab.gen == 7 && tab.state == "ready" && tab.err == "rejected" && tab.session.path == "/old"
			a.remoteTabMu.Unlock()
			if !intact {
				t.Fatal("retirement mutated identity before prior publication completed")
			}
			unblock()
			select {
			case <-finished:
			case <-time.After(3 * time.Second):
				t.Fatal("failure did not finish")
			}
			select {
			case <-retired:
			case <-time.After(3 * time.Second):
				t.Fatal("retirement did not finish")
			}
			records := events.recorded()
			if len(records) < 2 {
				t.Fatalf("missing ordered failure events: %v", records)
			}
			// The terminal failure follows its metadata; any retirement state follows both.
			if !strings.Contains(records[0], "remote-tab:updated") || !strings.Contains(records[1], "rejected") {
				t.Fatalf("publication order = %v", records)
			}
		})
	}
}

func TestRemoteResumeFailureRejectsLostOwnership(t *testing.T) {
	for _, kind := range []string{"generation", "selection", "route-revision", "path", "client", "replacement"} {
		t.Run(kind, func(t *testing.T) {
			client := &http.Client{}
			tab := &remoteTab{id: "remote-1", state: "ready", client: client, gen: 7, selectionRevision: 9,
				session: remoteTabSessionState{path: "/old"}, routing: remoteTabSessionRouting{currentPath: "/old", pathRevision: 11, running: map[string]bool{}},
			}
			log := &eventLog{}
			a := &App{remoteTabs: map[string]*remoteTab{tab.id: tab}, remoteEventHook: log.add}
			route := a.beginRemoteTabProvisionalResume(tab.id, tab, client, 7, "/target")
			switch kind {
			case "generation":
				tab.gen++
			case "selection":
				tab.selectionRevision++
			case "route-revision":
				tab.routing.pathRevision++
			case "path":
				tab.routing.currentPath = "/newer"
			case "client":
				tab.client = &http.Client{}
			case "replacement":
				a.remoteTabs[tab.id] = &remoteTab{id: tab.id, state: "ready", gen: 7, client: client}
			}
			beforeRoute := tab.routing.currentPath
			if !a.completeRemoteTabResumeFailure(tab.id, tab, client, 7, route, "obsolete") {
				t.Fatal("stale failure claimed completion")
			}
			if tab.err != "" || tab.routing.currentPath != beforeRoute || len(log.recorded()) != 0 {
				t.Fatalf("stale failure mutated or published: error=%q route=%q events=%v", tab.err, tab.routing.currentPath, log.recorded())
			}
		})
	}
}

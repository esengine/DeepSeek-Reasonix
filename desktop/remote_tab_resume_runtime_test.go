package main

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestProvisionalResumeClearsPreviousSessionJobsAndRollbackRestoresThem(t *testing.T) {
	const oldPath = "/sessions/old.jsonl"
	const targetPath = "/sessions/target.jsonl"
	client := &http.Client{}
	tab := &remoteTab{
		id: "remote-1", state: "ready", client: client, gen: 7,
		routing: remoteTabSessionRouting{currentPath: oldPath, running: map[string]bool{}},
		pendingEvents: map[string]json.RawMessage{
			"approval_request:old": json.RawMessage(`{"kind":"approval_request"}`),
		},
		runtime: remoteTabRuntimeState{
			running: true, turnStartedAt: 99, backgroundJobs: 3,
			pendingPrompt: true, cancelRequested: true, cancellable: true,
		},
	}
	a := &App{remoteTabs: map[string]*remoteTab{tab.id: tab}}
	route := a.beginRemoteTabProvisionalResume(tab.id, tab, client, tab.gen, targetPath)
	if !route.active || route.previousRuntime.backgroundJobs != 3 {
		t.Fatalf("provisional snapshot = %+v", route)
	}
	if tab.runtime.backgroundJobs != 0 || tab.runtime.running || tab.runtime.cancellable || tab.runtime.pendingPrompt || tab.runtime.cancelRequested || len(tab.pendingEvents) != 0 {
		t.Fatalf("provisional target retained previous foreground state: runtime=%+v pending=%d", tab.runtime, len(tab.pendingEvents))
	}
	if !a.rollbackRemoteTabProvisionalResume(tab.id, tab, client, tab.gen, route) {
		t.Fatal("provisional route did not roll back")
	}
	if tab.runtime.backgroundJobs != 3 || !tab.runtime.running || !tab.runtime.cancellable || len(tab.pendingEvents) != 1 {
		t.Fatalf("rollback did not restore previous foreground state: runtime=%+v pending=%d", tab.runtime, len(tab.pendingEvents))
	}
}

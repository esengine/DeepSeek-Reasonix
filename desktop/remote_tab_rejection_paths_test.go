package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestRemoteResumeFailurePathsPublishOneRestoredSnapshot(t *testing.T) {
	for _, kind := range []string{"http", "busy", "listing", "notfound", "transport"} {
		t.Run(kind, func(t *testing.T) {
			isolateDesktopUserDirs(t)
			const oldPath, targetPath = "/sessions/old.jsonl", "/sessions/target.jsonl"
			client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				code, body := http.StatusConflict, "rejected"
				if kind == "busy" {
					body = "while a turn is running"
				}
				if kind == "listing" {
					code = http.StatusInternalServerError
				}
				if kind == "notfound" {
					code, body = http.StatusOK, `[]`
				}
				if kind == "transport" {
					if req.URL.Path == "/resume" {
						return nil, errors.New("response lost")
					}
					code, body = http.StatusOK, `[{"name":"old","path":"/sessions/old.jsonl","title":"Old","current":true}]`
				}
				return &http.Response{StatusCode: code, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
			})}
			tab := &remoteTab{id: "remote-1", state: "ready", client: client, base: "http://fixture.invalid", gen: 7, selectionRevision: 9,
				session: remoteTabSessionState{name: "target", path: targetPath}, topicTitle: "Target",
				routing: remoteTabSessionRouting{currentPath: targetPath, pathRevision: 11, running: map[string]bool{}},
			}
			oldPending := json.RawMessage(`{"kind":"approval_request","callId":"old"}`)
			previous := &remoteTabOpenSelection{session: remoteTabSessionState{name: "old", path: oldPath}, topicTitle: "Old", currentPath: oldPath, revision: 9,
				pending: map[string]json.RawMessage{"old": oldPending}, runtime: remoteTabRuntimeState{running: true, cancellable: true, revision: 3},
			}
			a := &App{remoteTabs: map[string]*remoteTab{tab.id: tab}}
			failures := 0
			a.remoteEventHook = func(_ string, payload any) {
				state, ok := payload.(RemoteTabStateView)
				if !ok || state.Error == "" {
					return
				}
				failures++
				a.remoteTabMu.Lock()
				defer a.remoteTabMu.Unlock()
				if tab.session.name != "old" || tab.session.path != oldPath || tab.routing.currentPath != oldPath || tab.topicTitle != "Old" ||
					!tab.runtime.running || !tab.runtime.cancellable || string(tab.pendingEvents["old"]) != string(oldPending) || tab.err != state.Error {
					t.Errorf("failure exposed a partial identity/runtime/prompt restore: session=%+v route=%q title=%q runtime=%+v error=%q", tab.session, tab.routing.currentPath, tab.topicTitle, tab.runtime, tab.err)
				}
			}
			path := targetPath
			if kind == "listing" || kind == "notfound" {
				path = ""
			}
			a.resumeRemoteTabSessionPathForOpenSelection(tab.id, "target", path, "Target", 9, previous)
			if failures != 1 {
				t.Fatalf("failure publications = %d, want 1", failures)
			}
		})
	}
}

func TestRemoteRejectedResumePreservesProbedAuthoritativeSelection(t *testing.T) {
	isolateDesktopUserDirs(t)
	const previousPath = "/sessions/previous.jsonl"
	const targetPath = "/sessions/target.jsonl"
	const authoritativePath = "/sessions/authoritative.jsonl"
	client := &http.Client{}
	tab := &remoteTab{
		id: "remote-1", state: "ready", client: client, gen: 7, selectionRevision: 11,
		session: remoteTabSessionState{name: "previous", path: previousPath}, topicTitle: "Previous",
		routing: remoteTabSessionRouting{currentPath: previousPath, running: map[string]bool{}},
	}
	a := &App{remoteTabs: map[string]*remoteTab{tab.id: tab}}
	previous := &remoteTabOpenSelection{
		session: tab.session, topicTitle: tab.topicTitle, currentPath: previousPath, revision: tab.selectionRevision,
	}
	route := a.beginRemoteTabProvisionalResume(tab.id, tab, client, tab.gen, targetPath)
	route.previousSelection = previous
	handled := a.reconcileRemoteTabRejectedResume(
		tab.id, tab, client, tab.gen, route,
		serveSessionEntry{Name: "authoritative", Path: authoritativePath, Title: "Authoritative"},
		errors.New("resume response lost"),
	)
	if !handled {
		t.Fatal("authoritative reconciliation requested stale rollback")
	}
	a.remoteTabMu.Lock()
	gotPath, gotSession, gotTitle := tab.routing.currentPath, tab.session.path, tab.topicTitle
	a.remoteTabMu.Unlock()
	if gotPath != authoritativePath || gotSession != authoritativePath || gotTitle != "Authoritative" {
		t.Fatalf("ambiguous resume restored stale selection: route/session/title = %q/%q/%q", gotPath, gotSession, gotTitle)
	}
}

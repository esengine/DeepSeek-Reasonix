package main

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestRemotePresentedFileDeclaredRequiresTrustedPresentRecord(t *testing.T) {
	history := json.RawMessage(`[
		{"role":"tool","toolCallId":"present-1","toolName":"present","presentedFiles":[{"path":"dist/game.html","description":"game"}]},
		{"role":"tool","toolCallId":"plugin-1","toolName":"plugin.present","presentedFiles":[{"path":"dist/fake.html"}]},
		{"role":"assistant","toolCallId":"present-2","toolName":"present","presentedFiles":[{"path":"dist/fake-2.html"}]}
	]`)
	if !remotePresentedFileDeclared(history, "present-1", "dist/game.html") {
		t.Fatal("trusted present record was not recognized")
	}
	for _, test := range []struct{ call, path string }{
		{"present-1", "dist/other.html"},
		{"plugin-1", "dist/fake.html"},
		{"present-2", "dist/fake-2.html"},
		{"", "dist/game.html"},
	} {
		if remotePresentedFileDeclared(history, test.call, test.path) {
			t.Fatalf("untrusted remote record accepted: call=%q path=%q", test.call, test.path)
		}
	}
	if remotePresentedFileDeclared(json.RawMessage(`not-json`), "present-1", "dist/game.html") {
		t.Fatal("invalid history must fail closed")
	}
}

func TestRemoteWorkspaceArtifactDeclaredRequiresSuccessfulNativeMutation(t *testing.T) {
	history := json.RawMessage(`[
		{"role":"assistant","toolCalls":[
			{"id":"write-1","name":"write_file","arguments":"{\"path\":\"dist/report.md\"}"},
			{"id":"move-1","name":"move_file","arguments":"{\"source_path\":\"a.md\",\"destination_path\":\"dist/moved.md\"}"},
			{"id":"plugin-1","name":"plugin_write","arguments":"{\"path\":\"dist/fake.md\"}"}
		]},
		{"role":"tool","toolCallId":"write-1","toolName":"write_file","content":"wrote dist/report.md"},
		{"role":"tool","toolCallId":"move-1","toolName":"move_file","toolResultError":"denied"},
		{"role":"tool","toolCallId":"plugin-1","toolName":"plugin_write","content":"ok"}
	]`)
	if !remoteWorkspaceArtifactDeclared(history, "write-1", "dist/report.md") {
		t.Fatal("successful native mutation was not recognized")
	}
	for _, test := range []struct{ call, path string }{
		{"write-1", "dist/other.md"},
		{"move-1", "dist/moved.md"},
		{"plugin-1", "dist/fake.md"},
		{"", "dist/report.md"},
	} {
		if remoteWorkspaceArtifactDeclared(history, test.call, test.path) {
			t.Fatalf("untrusted remote artifact accepted: call=%q path=%q", test.call, test.path)
		}
	}
}

func TestRemotePresentedFilesFenceRejectsRouteAndConnectionChanges(t *testing.T) {
	client := &http.Client{}
	tab := &remoteTab{
		id:                "remote-present",
		ref:               RemoteTabRef{HostID: "host-a"},
		state:             "ready",
		client:            client,
		base:              "http://127.0.0.1:43120",
		gen:               7,
		selectionRevision: 3,
		capabilities:      map[string]bool{"present-files-v1": true},
		routing: remoteTabSessionRouting{
			currentPath: "/remote/workspace/.reasonix/sessions/one.jsonl",
		},
	}
	app := &App{remoteTabs: map[string]*remoteTab{tab.id: tab}}
	fence, err := app.requireRemotePresentedFiles(tab.id, "host-a")
	if err != nil {
		t.Fatalf("require remote present support: %v", err)
	}
	if !app.remotePresentedFilesFenceCurrent(fence) {
		t.Fatal("fresh remote present fence should be current")
	}

	for _, mutate := range []func(){
		func() { tab.gen++ },
		func() { tab.selectionRevision++ },
		func() { tab.client = &http.Client{} },
		func() { tab.routing.currentPath = "/remote/workspace/.reasonix/sessions/two.jsonl" },
		func() { tab.routing.rehydratingPath = tab.routing.currentPath },
	} {
		mutate()
		if app.remotePresentedFilesFenceCurrent(fence) {
			t.Fatal("stale remote present fence was accepted")
		}
		tab.client = client
		tab.gen = fence.generation
		tab.selectionRevision = fence.selectionRevision
		tab.routing.currentPath = fence.sessionPath
		tab.routing.rehydratingPath = ""
	}
}

func TestRemotePresentedAbsolutePathUsesSourceWorkspaceStyle(t *testing.T) {
	for _, test := range []struct{ workspace, presented, want string }{
		{"/srv/project", "dist/game.html", "/srv/project/dist/game.html"},
		{"/srv/project", "/tmp/report.pdf", "/tmp/report.pdf"},
		{`C:\work\project`, `dist\game.html`, `C:\work\project\dist\game.html`},
		{`C:\work\project`, `D:\out\report.pdf`, `D:\out\report.pdf`},
	} {
		if got := remotePresentedAbsolutePath(test.workspace, test.presented); got != test.want {
			t.Errorf("remotePresentedAbsolutePath(%q, %q) = %q, want %q", test.workspace, test.presented, got, test.want)
		}
	}
}

func TestConfinedRemoteWorkspacePath(t *testing.T) {
	for _, test := range []struct {
		workspace string
		requested string
		want      string
		allowed   bool
	}{
		{"/srv/project", "dist/report.md", "/srv/project/dist/report.md", true},
		{"/srv/project", "/srv/project/dist/report.md", "/srv/project/dist/report.md", true},
		{"/srv/project", "../secret.txt", "", false},
		{"/srv/project", "/srv/project-old/secret.txt", "", false},
		{`C:\work\project`, `dist\report.md`, `C:\work\project\dist\report.md`, true},
		{`C:\work\project`, `C:\work\project\dist\report.md`, `C:\work\project\dist\report.md`, true},
		{`C:\work\project`, `D:\secret.txt`, "", false},
	} {
		got, err := confinedRemoteWorkspacePath(test.workspace, test.requested)
		if test.allowed && (err != nil || got != test.want) {
			t.Errorf("confinedRemoteWorkspacePath(%q, %q) = %q, %v; want %q", test.workspace, test.requested, got, err, test.want)
		}
		if !test.allowed && err == nil {
			t.Errorf("confinedRemoteWorkspacePath(%q, %q) unexpectedly allowed %q", test.workspace, test.requested, got)
		}
	}
}

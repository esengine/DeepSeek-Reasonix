package main

import (
	"errors"
	"testing"
)

func TestRemoteTargetRejectsEveryDesktopLocalPathOperation(t *testing.T) {
	target := TargetDescriptor{Kind: TargetRemote, ID: "host_path_guard", Label: "Host path guard"}
	runtime := newRemoteWorkbenchTestRuntime()
	app, _ := newRemoteWorkbenchTestApp(t, target, runtime, nil)
	status, err := app.CreateRemoteWorkspaceSession(RemoteCreateWorkspaceSessionInput{
		PrimaryDirectoryRef: "dir_primary",
		TopicTitle:          "Remote path guard",
	})
	if err != nil {
		t.Fatal(err)
	}

	checks := []struct {
		name string
		call func() error
	}{
		{name: "open workspace path", call: func() error { return app.OpenWorkspacePathForTab(status.TabID, "README.md") }},
		{name: "reveal workspace path", call: func() error { return app.RevealWorkspacePathForTab(status.TabID, "README.md") }},
		{name: "reveal arbitrary display path", call: func() error { return app.RevealPath(runtime.workspace.DisplayPath) }},
		{name: "external opener", call: func() error { return app.OpenWorkspaceInExternalOpenerForTab(status.TabID, "editor") }},
		{name: "AutoResearch task path", call: func() error { return app.AutoResearchOpenTask(status.TabID) }},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			if err := check.call(); !errors.Is(err, ErrRemoteLocalPathOperation) {
				t.Fatalf("error = %v, want ErrRemoteLocalPathOperation", err)
			}
		})
	}

	if _, err := app.OpenGlobalTab("remote-topic"); err == nil {
		t.Fatal("OpenGlobalTab succeeded while a Remote Host target was selected")
	}
}

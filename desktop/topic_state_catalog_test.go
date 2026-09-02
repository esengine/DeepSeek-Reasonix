package main

import (
	"testing"

	"reasonix/internal/config"
)

// A remote project group's qualified root ("remote-project:<host>:<workspace>")
// is not a filesystem path. Listing must answer with an empty page instead of
// building a topic-state scope from it — on Windows the colon is an invalid
// path component and the legacy metadata read failed with a user-visible
// "topic metadata is unavailable (io)" toast right after the connect wizard.
func TestListProjectTopicsAnswersRemoteRootWithEmptyPage(t *testing.T) {
	isolateDesktopUserDirs(t)
	app := NewApp()
	page, err := app.ListProjectTopics(ProjectTopicPageRequest{
		Scope:         "project",
		WorkspaceRoot: "remote-project:gpu-box:/home/dev/app",
		Limit:         50,
	})
	if err != nil {
		t.Fatalf("remote root listing failed: %v", err)
	}
	if page.Items == nil || len(page.Items) != 0 {
		t.Fatalf("remote root page items = %#v, want an empty page", page.Items)
	}
}

func TestListProjectTreeSkipsRemoteTopicState(t *testing.T) {
	isolateDesktopUserDirs(t)
	const remoteRoot = "remote-project:gpu-box:/home/dev/app"
	if err := editUserConfig(func(c *config.Config) error {
		if err := c.UpsertRemoteHost(config.RemoteHostEntry{Name: "gpu-box", Host: "127.0.0.1"}); err != nil {
			return err
		}
		return c.UpsertRemoteProject(config.RemoteProjectEntry{HostID: "gpu-box", Workspace: "/home/dev/app"})
	}); err != nil {
		t.Fatal(err)
	}

	found := false
	for _, node := range NewApp().ListProjectTree() {
		if node.Remote == nil {
			continue
		}
		found = true
		if len(node.Children) != 0 {
			t.Fatalf("remote project children = %#v, want none from local topic state", node.Children)
		}
	}
	if !found {
		t.Fatal("compatibility tree omitted the remote project group")
	}

	desktopTopicState.mu.Lock()
	opened := desktopTopicState.scopes[config.DesktopTopicStatePath(remoteRoot)] != nil
	desktopTopicState.mu.Unlock()
	if opened {
		t.Fatalf("compatibility tree opened local topic state for virtual root %q", remoteRoot)
	}
}

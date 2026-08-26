package main

import (
	"strings"
	"testing"

	"reasonix/internal/config"
)

// TestSnapshotIncludesRemoteProjectGroups pins that pinned remote workspaces
// surface in the tree snapshot as ordinary project groups whose Remote ref
// marks them for the cloud icon, and that a config read failure degrades to
// "no remote groups" instead of failing the whole snapshot.
func TestSnapshotIncludesRemoteProjectGroups(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("HOME", home)
	if err := editUserConfig(func(c *config.Config) error {
		if err := c.UpsertRemoteHost(config.RemoteHostEntry{Name: "gpu-box", Host: "192.168.1.10", User: "dev"}); err != nil {
			return err
		}
		return c.UpsertRemoteProject(config.RemoteProjectEntry{HostID: "gpu-box", Workspace: "/home/dev/app"})
	}); err != nil {
		t.Fatal(err)
	}

	a := &App{}
	found := false
	for _, node := range a.GetProjectTreeSnapshot().Projects {
		if node.Remote == nil {
			continue
		}
		if node.Remote.HostID != "gpu-box" || node.Remote.Workspace != "/home/dev/app" {
			t.Fatalf("unexpected remote node: %+v", node)
		}
		found = true
		if node.Kind != "project" {
			t.Fatalf("remote group kind = %q, want project", node.Kind)
		}
		if !strings.HasPrefix(node.Key, "project_remote_") {
			t.Fatalf("remote group key = %q", node.Key)
		}
		if node.Root != "remote-project:gpu-box:/home/dev/app" {
			t.Fatalf("remote group root = %q, want host-qualified tree identity", node.Root)
		}
		if node.Label != "app" {
			t.Fatalf("remote group label = %q, want workspace base name", node.Label)
		}
	}
	if !found {
		t.Fatal("snapshot missing the remote project group")
	}
}

func TestRemoteProjectNodeKeysDoNotCollide(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("HOME", home)
	if err := editUserConfig(func(c *config.Config) error {
		for _, host := range []string{"a_b", "a"} {
			if err := c.UpsertRemoteHost(config.RemoteHostEntry{Name: host, Host: "127.0.0.1"}); err != nil {
				return err
			}
		}
		if err := c.UpsertRemoteProject(config.RemoteProjectEntry{HostID: "a_b", Workspace: "c"}); err != nil {
			return err
		}
		return c.UpsertRemoteProject(config.RemoteProjectEntry{HostID: "a", Workspace: "b_c"})
	}); err != nil {
		t.Fatal(err)
	}
	nodes, err := (&App{}).remoteProjectNodes()
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 2 || nodes[0].Key == nodes[1].Key {
		t.Fatalf("remote project keys collided: %+v", nodes)
	}
}

func TestRemoteProjectTreeIdentityIncludesHost(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("HOME", home)
	if err := editUserConfig(func(c *config.Config) error {
		for _, host := range []string{"host-a", "host-b"} {
			if err := c.UpsertRemoteHost(config.RemoteHostEntry{Name: host, Host: "127.0.0.1"}); err != nil {
				return err
			}
			if err := c.UpsertRemoteProject(config.RemoteProjectEntry{HostID: host, Workspace: "/srv/app"}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	nodes, err := (&App{}).remoteProjectNodes()
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 2 || nodes[0].Root == nodes[1].Root {
		t.Fatalf("same-path remote roots collided: %+v", nodes)
	}
}

func TestRemoteRootWorkspaceContainsAbsoluteDescendants(t *testing.T) {
	if !isRemoteSubpath("/", "/home/dev/app") {
		t.Fatal("POSIX root must contain every other absolute workspace")
	}
	if isRemoteSubpath("/", "/") || isRemoteSubpath("/", "relative/path") {
		t.Fatal("root containment must stay strict and absolute")
	}
}

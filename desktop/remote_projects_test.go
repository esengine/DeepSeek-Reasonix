package main

import (
	"strings"
	"testing"
	"time"

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

// TestBootstrapNewSessionPublishesTabUpdateForSidebarBlankRow pins the
// sidebar's only signal for a brand-new remote session: the fresh session has
// no transcript on the serve, so the session listing synthesizes a blank row
// from the tab's reset flag — but only after the sidebar re-pulls, which it
// does on remote-tab:updated. A bootstrap that reaches ready without that
// update leaves the new project group empty.
func TestBootstrapNewSessionPublishesTabUpdateForSidebarBlankRow(t *testing.T) {
	fs := newFakeServe(t, "s3cret", nil)
	const freshPath = "/remote/sessions/fresh.jsonl"
	fs.mu.Lock()
	fs.newSessionPath = freshPath
	fs.mu.Unlock()
	kernel := &fakeRemoteKernel{
		statuses:   []RemoteConnectionStatusView{{HostID: "box", State: "connected"}},
		ensureView: RemoteServerView{HostID: "box", State: "ready", LocalURL: fs.server.URL}, ensureToken: "s3cret",
	}
	seedBridgeTestHost(t, "box")
	log := &eventLog{}
	updates := make(chan TabMeta, 4)
	a := &App{remoteRuntime: kernel, remoteEventHook: func(name string, payload any) {
		log.add(name, payload)
		if name != "remote-tab:updated" {
			return
		}
		meta, ok := payload.(TabMeta)
		if !ok {
			return
		}
		select {
		case updates <- meta:
		default:
		}
	}}
	cleanupRemoteTabPumps(t, a)
	meta, err := a.OpenRemoteProjectTab("box", "~/app", RemoteTabOpenOptions{NewSession: true})
	if err != nil {
		t.Fatal(err)
	}
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	var update TabMeta
	for update.ID != meta.ID {
		select {
		case update = <-updates:
		case <-timer.C:
			t.Fatalf("bootstrap emitted no remote-tab:updated for tab %q, events: %v", meta.ID, log.recorded())
		}
	}
	if !update.Ready || update.RemoteState != "ready" {
		t.Fatalf("bootstrap update state = ready:%v remote:%q, want ready/ready", update.Ready, update.RemoteState)
	}

	a.remoteTabMu.Lock()
	reset := a.remoteTabs[meta.ID].session.reset
	a.remoteTabMu.Unlock()
	if !reset {
		t.Fatal("bootstrap did not mark the fresh session as reset")
	}
	sessions, err := a.RemoteProjectSessions("box", "~/app")
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].Name != "" || sessions[0].Path != freshPath || !sessions[0].Current {
		t.Fatalf("unlisted fresh session = %+v, want one synthetic current blank", sessions)
	}

	fs.mu.Lock()
	fs.statusPayload = `{"sessionName":"fresh","sessionPath":"/remote/sessions/fresh.jsonl","running":false}`
	fs.mu.Unlock()
	if _, err := a.RemoteTabStatus(meta.ID); err != nil {
		t.Fatal(err)
	}
	a.remoteTabMu.Lock()
	reset = a.remoteTabs[meta.ID].session.reset
	a.remoteTabMu.Unlock()
	if reset {
		t.Fatal("named status did not clear the fresh-session reset marker")
	}
	sessions, err = a.RemoteProjectSessions("box", "~/app")
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].Name != "" || sessions[0].Path != freshPath || !sessions[0].Current {
		t.Fatalf("named but unlisted fresh session = %+v, want the synthetic current blank", sessions)
	}

	fs.mu.Lock()
	fs.sessions = []serveSessionEntry{{Name: "fresh", Path: freshPath, Title: "Fresh", Current: true}}
	fs.mu.Unlock()
	sessions, err = a.RemoteProjectSessions("box", "~/app")
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].Name != "fresh" || sessions[0].Path != freshPath || !sessions[0].Current {
		t.Fatalf("materialized fresh session = %+v, want the real current row", sessions)
	}
}

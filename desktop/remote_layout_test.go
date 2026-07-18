package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/runtimeapi"
)

func remoteLayoutTestRuntime() *remoteWorkbenchV1TestRuntime {
	runtime := newRemoteWorkbenchV1TestRuntime()
	workspaceA := runtimeapi.Workspace{ID: "opaque/../workspace-a", Name: "Host Workspace A", DisplayPath: "/srv/host/a"}
	workspaceB := runtimeapi.Workspace{ID: `opaque\workspace-b`, Name: "Host Workspace B", DisplayPath: "/srv/host/b"}
	runtime.workspaces = []runtimeapi.Workspace{workspaceA, workspaceB}
	runtime.topics[workspaceA.ID] = []runtimeapi.TopicSummary{
		{TopicID: "topic_new", Title: "New activity", LastActivityMillis: 200},
		{TopicID: "topic_old", Title: "Old activity", LastActivityMillis: 100},
	}
	runtime.topics[workspaceB.ID] = []runtimeapi.TopicSummary{
		{TopicID: "topic_b", Title: "Topic B", LastActivityMillis: 150},
	}
	runtime.sessions[workspaceA.ID] = []runtimeapi.SessionSummary{
		{Session: runtimeapi.SessionRef{WorkspaceID: workspaceA.ID, SessionID: "session_new"}, TopicID: "topic_new", Title: "New session", LastActivityMillis: 200},
		{Session: runtimeapi.SessionRef{WorkspaceID: workspaceA.ID, SessionID: "session_old"}, TopicID: "topic_old", Title: "Old session", LastActivityMillis: 100},
	}
	return runtime
}

func newRemoteLayoutTestApp(t *testing.T, store *RemoteHostStore, host RemoteHostEntry, runtime runtimeapi.RuntimeAPI) *App {
	t.Helper()
	target := TargetDescriptor{Kind: TargetRemote, ID: host.ID, Label: host.Label}
	manager, err := NewTargetManager(
		TargetConnectorFunc(func(context.Context, TargetDescriptor) (TargetAdapter, error) {
			return nil, errors.New("unexpected target switch")
		}),
		&remoteWorkbenchTestAdapter{target: target, runtime: runtime},
		TargetManagerOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	app := &App{ctx: context.Background()}
	setRemoteWorkbenchTestEmitter(app, func(context.Context, string, ...interface{}) {})
	app.readyHook = func() {}
	app.projectTreeChangedHook = func() {}
	installRemoteAppTestState(t, app, store, manager)
	return app
}

func remoteLayoutNodeByRoot(t *testing.T, nodes []ProjectNode, root string) ProjectNode {
	t.Helper()
	for _, node := range nodes {
		if node.Root == root {
			return node
		}
	}
	t.Fatalf("Remote project tree has no workspace %q: %#v", root, nodes)
	return ProjectNode{}
}

func TestRemoteLayoutPersistsPerHostAndNeverTouchesLocalProjectRegistry(t *testing.T) {
	localProjectsPath := filepath.Join(desktopConfigDir(), desktopProjectsFile)
	if err := os.MkdirAll(filepath.Dir(localProjectsPath), 0o755); err != nil {
		t.Fatal(err)
	}
	localSentinel := []byte("{\n  \"projects\": [{\"root\": \"/desktop/local-only\", \"topics\": [\"local_topic\"]}]\n}\n")
	if err := os.WriteFile(localProjectsPath, localSentinel, 0o600); err != nil {
		t.Fatal(err)
	}

	storePath := filepath.Join(t.TempDir(), "remote-hosts.json")
	store, err := NewRemoteHostStore(storePath)
	if err != nil {
		t.Fatal(err)
	}
	hostA := newRemoteAppTestHost(t, store, "layout-a", "Layout A")
	hostB := newRemoteAppTestHost(t, store, "layout-b", "Layout B")
	appA := newRemoteLayoutTestApp(t, store, hostA, remoteLayoutTestRuntime())

	const workspaceA = "opaque/../workspace-a"
	const workspaceB = `opaque\workspace-b`
	if err := appA.RenameProject(workspaceA, "Client API"); err != nil {
		t.Fatal(err)
	}
	if err := appA.SetProjectColor(workspaceA, "BLUE"); err != nil {
		t.Fatal(err)
	}
	if err := appA.ReorderProjects([]string{workspaceA, workspaceB}); err != nil {
		t.Fatal(err)
	}
	if err := appA.SetProjectPinned(workspaceB, true); err != nil {
		t.Fatal(err)
	}
	if err := appA.SetTopicPinned("topic_old", true); err != nil {
		t.Fatal(err)
	}

	treeA := appA.ListProjectTree()
	if len(treeA) != 2 || treeA[0].Root != workspaceB || !treeA[0].Pinned || treeA[1].Root != workspaceA {
		t.Fatalf("Remote project order/pinning = %#v", treeA)
	}
	projectA := remoteLayoutNodeByRoot(t, treeA, workspaceA)
	if projectA.Label != "Client API" || projectA.ProjectColor != "blue" {
		t.Fatalf("Remote workspace presentation = %#v", projectA)
	}
	if len(projectA.Children) != 2 || projectA.Children[0].TopicID != "topic_old" || !projectA.Children[0].Pinned || projectA.Children[1].TopicID != "topic_new" {
		t.Fatalf("Remote pinned topic order = %#v", projectA.Children)
	}
	if projectA.Children[0].ProjectColor != "blue" || len(projectA.Children[0].Children) != 1 || projectA.Children[0].Children[0].ProjectColor != "blue" {
		t.Fatalf("Remote project color was not projected through topic/session nodes: %#v", projectA.Children[0])
	}

	hostAAfter, found, err := store.Get(hostA.ID)
	if err != nil || !found {
		t.Fatalf("load Host A after layout writes: found=%v err=%v", found, err)
	}
	expectedRef, err := remoteLayoutRefForHost(hostA.ID)
	if err != nil {
		t.Fatal(err)
	}
	if hostAAfter.LayoutRef != expectedRef || filepath.Base(hostAAfter.LayoutRef) != hostAAfter.LayoutRef {
		t.Fatalf("Host A layoutRef = %q, want %q", hostAAfter.LayoutRef, expectedRef)
	}
	layoutPath, err := remoteLayoutPath(store, hostAAfter)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(layoutPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("Remote layout permissions = %04o, want 0600", info.Mode().Perm())
	}
	if bytes.Contains([]byte(layoutPath), []byte(workspaceA)) || bytes.Contains([]byte(layoutPath), []byte(workspaceB)) {
		t.Fatalf("opaque workspace identity leaked into Desktop path %q", layoutPath)
	}
	localAfter, err := os.ReadFile(localProjectsPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(localAfter, localSentinel) {
		t.Fatalf("Remote preference calls modified local project registry:\n%s", localAfter)
	}

	// A fresh App/TargetManager process view loads the same target's persisted
	// layout, while another saved Host with identical Host catalogs remains
	// completely isolated and uses Host defaults.
	restartedA := newRemoteLayoutTestApp(t, store, hostAAfter, remoteLayoutTestRuntime())
	restartedTree := restartedA.ListProjectTree()
	restartedProjectA := remoteLayoutNodeByRoot(t, restartedTree, workspaceA)
	if len(restartedTree) != 2 || restartedTree[0].Root != workspaceB || restartedProjectA.Label != "Client API" || restartedProjectA.ProjectColor != "blue" || restartedProjectA.Children[0].TopicID != "topic_old" {
		t.Fatalf("Remote layout did not survive Desktop restart: %#v", restartedTree)
	}
	isolatedB := newRemoteLayoutTestApp(t, store, hostB, remoteLayoutTestRuntime())
	treeB := isolatedB.ListProjectTree()
	projectBWorkspaceA := remoteLayoutNodeByRoot(t, treeB, workspaceA)
	if len(treeB) != 2 || treeB[0].Root != workspaceA || treeB[0].Pinned || projectBWorkspaceA.Label != "Host Workspace A" || projectBWorkspaceA.ProjectColor != "" || projectBWorkspaceA.Children[0].TopicID != "topic_new" || projectBWorkspaceA.Children[0].Pinned {
		t.Fatalf("Host B inherited Host A Desktop preferences: %#v", treeB)
	}
	hostBAfter, found, err := store.Get(hostB.ID)
	if err != nil || !found || hostBAfter.LayoutRef != "" {
		t.Fatalf("read-only Host B tree unexpectedly persisted layout: host=%#v found=%v err=%v", hostBAfter, found, err)
	}
}

func TestRemoteLayoutRejectsInvalidOrderWithoutChangingPersistedState(t *testing.T) {
	store := newRemoteAppTestStore(t)
	host := newRemoteAppTestHost(t, store, "layout-order", "Layout order")
	app := newRemoteLayoutTestApp(t, store, host, remoteLayoutTestRuntime())
	const workspaceA = "opaque/../workspace-a"
	const workspaceB = `opaque\workspace-b`
	if err := app.ReorderProjects([]string{workspaceB, workspaceA}); err != nil {
		t.Fatal(err)
	}
	host, _, _ = store.Get(host.ID)
	path, err := remoteLayoutPath(store, host)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, order := range [][]string{{workspaceA}, {workspaceA, workspaceA}, {workspaceA, "missing"}} {
		if err := app.ReorderProjects(order); err == nil {
			t.Fatalf("ReorderProjects(%q) succeeded", order)
		}
		after, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if !bytes.Equal(after, before) {
			t.Fatalf("invalid Remote order %q changed persisted layout", order)
		}
	}
}

func TestRemoteLayoutConcurrentStoreInstancesDoNotLoseIndependentUpdates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "remote-hosts.json")
	storeA, err := NewRemoteHostStore(path)
	if err != nil {
		t.Fatal(err)
	}
	host := newRemoteAppTestHost(t, storeA, "layout-concurrent", "Layout concurrent")
	storeB, err := NewRemoteHostStore(path)
	if err != nil {
		t.Fatal(err)
	}
	appA := newRemoteLayoutTestApp(t, storeA, host, remoteLayoutTestRuntime())
	appB := newRemoteLayoutTestApp(t, storeB, host, remoteLayoutTestRuntime())
	start := make(chan struct{})
	errorsByOperation := make(chan error, 2)
	go func() {
		<-start
		errorsByOperation <- appA.RenameProject("opaque/../workspace-a", "Concurrent title")
	}()
	go func() {
		<-start
		errorsByOperation <- appB.SetProjectColor("opaque/../workspace-a", "purple")
	}()
	close(start)
	for range 2 {
		if err := <-errorsByOperation; err != nil {
			t.Fatal(err)
		}
	}
	document, err := loadRemoteLayout(storeA, host.ID)
	if err != nil {
		t.Fatal(err)
	}
	if document.WorkspaceTitles["opaque/../workspace-a"] != "Concurrent title" || document.WorkspaceColors["opaque/../workspace-a"] != "purple" {
		t.Fatalf("concurrent Remote layout writes lost an update: %#v", document)
	}
}

func TestRemoteLayoutCorruptionAndUnsafeReferencesFailClosed(t *testing.T) {
	t.Run("corrupt document", func(t *testing.T) {
		store := newRemoteAppTestStore(t)
		host := newRemoteAppTestHost(t, store, "layout-corrupt", "Layout corrupt")
		app := newRemoteLayoutTestApp(t, store, host, remoteLayoutTestRuntime())
		if err := app.RenameProject("opaque/../workspace-a", "Before corruption"); err != nil {
			t.Fatal(err)
		}
		host, _, _ = store.Get(host.ID)
		path, err := remoteLayoutPath(store, host)
		if err != nil {
			t.Fatal(err)
		}
		corrupt := []byte(`{"version":1,"hostId":"wrong","unknown":true}`)
		if err := os.WriteFile(path, corrupt, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := app.SetProjectColor("opaque/../workspace-a", "red"); !errors.Is(err, ErrRemoteLayoutCorrupt) {
			t.Fatalf("SetProjectColor error = %v, want ErrRemoteLayoutCorrupt", err)
		}
		after, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(after, corrupt) {
			t.Fatalf("corrupt Remote layout was overwritten: %s", after)
		}
	})

	t.Run("escaping layoutRef", func(t *testing.T) {
		root := t.TempDir()
		store, err := NewRemoteHostStore(filepath.Join(root, "store", "remote-hosts.json"))
		if err != nil {
			t.Fatal(err)
		}
		host := newRemoteAppTestHost(t, store, "layout-escape", "Layout escape")
		host.LayoutRef = "../escape.json"
		if err := store.Upsert(host); err != nil {
			t.Fatal(err)
		}
		app := newRemoteLayoutTestApp(t, store, host, remoteLayoutTestRuntime())
		if err := app.RenameProject("opaque/../workspace-a", "Escaped"); !errors.Is(err, ErrRemoteLayoutUnsafe) {
			t.Fatalf("RenameProject error = %v, want ErrRemoteLayoutUnsafe", err)
		}
		if _, err := os.Stat(filepath.Join(root, "escape.json")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("unsafe layoutRef created an escaping file: %v", err)
		}
	})

	t.Run("symlink file", func(t *testing.T) {
		root := t.TempDir()
		store, err := NewRemoteHostStore(filepath.Join(root, "remote-hosts.json"))
		if err != nil {
			t.Fatal(err)
		}
		host := newRemoteAppTestHost(t, store, "layout-link", "Layout link")
		ref, err := remoteLayoutRefForHost(host.ID)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.UpdateLayoutRef(host.ID, ref); err != nil {
			t.Fatal(err)
		}
		host.LayoutRef = ref
		victim := filepath.Join(root, "victim.json")
		victimBefore := []byte("victim must remain unchanged")
		if err := os.WriteFile(victim, victimBefore, 0o600); err != nil {
			t.Fatal(err)
		}
		path, err := remoteLayoutPath(store, host)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(victim, path); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		app := newRemoteLayoutTestApp(t, store, host, remoteLayoutTestRuntime())
		if err := app.SetProjectPinned("opaque/../workspace-a", true); !errors.Is(err, ErrRemoteLayoutUnsafe) {
			t.Fatalf("SetProjectPinned error = %v, want ErrRemoteLayoutUnsafe", err)
		}
		victimAfter, err := os.ReadFile(victim)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(victimAfter, victimBefore) {
			t.Fatalf("unsafe layout symlink target changed: %q", victimAfter)
		}
	})
}

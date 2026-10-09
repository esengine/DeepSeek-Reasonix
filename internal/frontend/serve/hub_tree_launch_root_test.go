package serve

import (
	"net/http/httptest"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/safety/permission"
	"reasonix/internal/session/control"
	"reasonix/internal/state/sessionstore"
)

// A pane adopted at launch is not a folder anyone picked, so nothing listed it
// once the next launch opened another one — and every conversation held there
// stayed on disk with no row to open it from.
func TestTreeKeepsTheFolderAFirstTurnWasHeldIn(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	launchA, projectB := testenv.TempDir(t), testenv.TempDir(t)

	reg := tool.NewRegistry()
	reg.Add(agent.NewAskTool())
	executor := agent.New(&askingProvider{}, reg, sessionstore.NewSession("sys"), agent.Options{MaxSteps: 4}, event.Discard)
	bc := NewBroadcaster()
	asked := make(chan event.Ask, 1)
	ctrl := control.New(control.Options{Runner: executor, Executor: executor,
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.AskRequest {
				asked <- e.Ask
			}
			bc.Emit(e)
		}),
		Policy: permission.New("ask", nil, nil, nil), SessionDir: SessionDirFor(launchA), WorkspaceRoot: launchA})
	hub := NewHub(HubOptions{})
	rt, err := hub.Adopt(New(ctrl, bc, config.ServeConfig{}), bc)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(operatorHandler(hub))
	postLaunchJSON(t, ts.URL+"/rt/"+rt.ID+"/submit", `{"input":"do the task"}`, nil)
	select {
	case <-asked:
	case <-time.After(10 * time.Second):
		t.Fatal("no ask")
	}
	ts.Close()
	hub.Shutdown()

	// "restart": the user has picked project B in the meantime / launch lands there
	if err := addRememberedWorkspace(t.Context(), projectB); err != nil {
		t.Fatal(err)
	}
	hub2 := NewHub(HubOptions{})
	hubRuntime(t, hub2, projectB)
	ts2 := httptest.NewServer(operatorHandler(hub2))
	defer ts2.Close()
	tree := hubGet[[]treeWorkspace](t, ts2, "/tree")
	var held int
	for _, w := range tree {
		if filepath.Clean(w.Root) == filepath.Clean(launchA) {
			held = len(w.Sessions)
		}
	}
	if held != 1 {
		t.Fatalf("the launch folder shows %d conversations after a restart, want the 1 it holds; tree = %+v", held, tree)
	}
}

func TestKeepUsedWorkspaceLeavesOrderAndLaunchAlone(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	first, second, used := testenv.TempDir(t), testenv.TempDir(t), testenv.TempDir(t)
	for _, dir := range []string{second, first} {
		if err := addRememberedWorkspace(t.Context(), dir); err != nil {
			t.Fatal(err)
		}
	}
	keepUsedWorkspace(used)
	keepUsedWorkspace(used)
	keepUsedWorkspace("")
	list, _ := readWorkspaceList(workspacesPath())
	want := []string{first, second, used}
	if !slices.Equal(list.Paths, want) || list.Launch != first {
		t.Fatalf("list = %+v, want paths %v launching %s", list, want, first)
	}
}

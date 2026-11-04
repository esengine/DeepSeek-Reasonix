package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"

	"reasonix/internal/base/testenv"
)

func TestRemoveUnbookedRemoteWorkspacePersistsWithoutDialing(t *testing.T) {
	front := bookServer(t, &stubAttacher{attach: func(string, string) (RemoteEndpoint, func(), error) {
		t.Error("removing a sidebar directory must not dial the remote")
		return RemoteEndpoint{}, func() {}, nil
	}})
	bookPost(t, front, "/remotes", `{"name":"gpu-box","host":"10.0.0.4","user":"ada","workspaces":["/srv/keep"]}`)
	for range 2 {
		if resp := bookPost(t, front, "/remotes/gpu-box/workspaces/remove", `{"path":"/root"}`); resp.StatusCode != http.StatusNoContent {
			t.Fatalf("remove status = %d", resp.StatusCode)
		}
	}
	entry := bookEntry(t, "gpu-box")
	if entry.Host != "10.0.0.4" || entry.User != "ada" || !reflect.DeepEqual(entry.WorkspaceList(), []string{"/srv/keep"}) {
		t.Fatalf("removal changed host settings: %+v", entry)
	}
	readHidden := func() []string {
		t.Helper()
		resp, err := http.Get(front.URL + "/remotes")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var rows []struct {
			HiddenWorkspaces []string `json:"hiddenWorkspaces"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil {
			t.Fatal(err)
		}
		if len(rows) != 1 {
			t.Fatalf("hosts = %+v", rows)
		}
		return rows[0].HiddenWorkspaces
	}
	if got := readHidden(); !reflect.DeepEqual(got, []string{"/root"}) {
		t.Fatalf("removed directory was not remembered: %v", got)
	}
	bookPost(t, front, "/remotes", `{"name":"gpu-box","host":"10.0.0.5","user":"ada","workspaces":["/srv/keep"]}`)
	if got := readHidden(); !reflect.DeepEqual(got, []string{"/root"}) {
		t.Fatalf("editing host resurrected directory: %v", got)
	}
	bookPost(t, front, "/remotes/gpu-box/workspaces", `{"path":"/root"}`)
	if got := readHidden(); len(got) != 0 {
		t.Fatalf("explicit add did not restore directory: %v", got)
	}
}

func TestRemoteWorkspaceGroupRemovalChecksEveryPaneBeforeChangingTheBook(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	var running atomic.Bool
	running.Store(true)
	far := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"running": running.Load() && r.URL.Path == "/rt/worktree/status"})
	}))
	defer far.Close()
	hub := NewHub(HubOptions{Remote: &stubAttacher{}})
	defer hub.Shutdown()
	front := httptest.NewServer(operatorHandler(hub))
	defer front.Close()
	bookPost(t, front, "/remotes", `{"name":"gpu-box","host":"example.test","workspaces":["/project","/worktree","/keep"]}`)
	for _, path := range []string{"/project", "/worktree"} {
		if _, err := hub.OpenRemote(RemoteEndpoint{Host: "gpu-box", Workspace: path, Addr: far.Listener.Addr().String(), Token: "t", Base: "/rt" + path}, nil); err != nil {
			t.Fatal(err)
		}
	}
	body := `{"path":"/project","paths":["/project","/worktree"]}`
	resp := bookPost(t, front, "/remotes/gpu-box/workspaces/remove", body)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("busy group removal = %d", resp.StatusCode)
	}
	entry := bookEntry(t, "gpu-box")
	if len(entry.HiddenWorkspaces) != 0 || len(entry.WorkspaceList()) != 3 || len(hub.Runtimes()) != 2 {
		t.Fatalf("busy group partially removed: %+v, panes=%d", entry, len(hub.Runtimes()))
	}
	running.Store(false)
	resp = bookPost(t, front, "/remotes/gpu-box/workspaces/remove", body)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("idle group removal = %d", resp.StatusCode)
	}
	entry = bookEntry(t, "gpu-box")
	if !reflect.DeepEqual(entry.HiddenWorkspaces, []string{"/project", "/worktree"}) || !reflect.DeepEqual(entry.WorkspaceList(), []string{"/keep"}) || len(hub.Runtimes()) != 0 {
		t.Fatalf("idle group not removed: %+v, panes=%d", entry, len(hub.Runtimes()))
	}
	rememberRemoteWorkspace("gpu-box", "/project")
	entry = bookEntry(t, "gpu-box")
	if !reflect.DeepEqual(entry.HiddenWorkspaces, []string{"/worktree"}) {
		t.Fatalf("explicit reopen did not restore project: %+v", entry)
	}
}

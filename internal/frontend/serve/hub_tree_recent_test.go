package serve

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/state/sessionstore"
)

func TestRecentTreePagesAcrossProjectsBeyondSidebarCap(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	h := NewHub(HubOptions{})
	roots := []string{testenv.TempDir(t), testenv.TempDir(t)}
	for i, root := range roots {
		rememberWorkspace(root)
		seedAged(t, root, []int{341, 7}[i])
	}
	srv := httptest.NewServer(operatorHandler(h))
	defer srv.Close()
	seen := map[string]bool{}
	var previous int64 = 1<<63 - 1
	for offset := 0; offset < 348; offset += 50 {
		page := hubGet[[]treeWorkspace](t, srv, fmt.Sprintf("/tree?order=recent&scope=active&limit=50&offset=%d", offset))
		rows := recentRows(page)
		want := min(50, 348-offset)
		if len(rows) != want {
			t.Fatalf("offset %d: %d rows, want %d", offset, len(rows), want)
		}
		remaining := 0
		for _, ws := range page {
			remaining += ws.Remaining
		}
		if remaining != max(0, 348-offset-50) {
			t.Fatalf("offset %d: remaining %d", offset, remaining)
		}
		for _, row := range rows {
			if seen[row.session.Path] || row.session.LastActiveAt > previous {
				t.Fatalf("duplicate or out-of-order row at %d: %+v", offset, row.session)
			}
			seen[row.session.Path], previous = true, row.session.LastActiveAt
		}
	}
	if len(seen) != 348 {
		t.Fatalf("reached %d conversations", len(seen))
	}
}

func TestRecentTreeUsesActivityBeforeWindowNotManualRankOrPins(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	root := testenv.TempDir(t)
	rememberWorkspace(root)
	paths := seedAged(t, root, 4)
	if err := sessionstore.SetSessionRank(paths[3], 1024); err != nil {
		t.Fatal(err)
	}
	if err := sessionstore.SetSessionPinned(paths[0], true); err != nil {
		t.Fatal(err)
	}
	if err := sessionstore.SetSessionLineageArchived(paths[2], true); err != nil {
		t.Fatal(err)
	}
	h := NewHub(HubOptions{})
	srv := httptest.NewServer(operatorHandler(h))
	defer srv.Close()
	page := treeFor(t, srv, "?order=recent&scope=active&limit=1")
	if len(page.Sessions) != 1 || page.Sessions[0].Path != paths[3] || page.Remaining != 2 {
		t.Fatalf("recent first page: %+v", page)
	}
	last := treeFor(t, srv, "?order=recent&scope=active&limit=1&offset=2")
	if len(last.Sessions) != 1 || last.Sessions[0].Path != paths[0] || last.Remaining != 0 {
		t.Fatalf("recent last page: %+v", last)
	}
	restarted := httptest.NewServer(operatorHandler(NewHub(HubOptions{})))
	defer restarted.Close()
	restored := treeFor(t, restarted, "?order=recent&scope=active&limit=1")
	if len(restored.Sessions) != 1 || restored.Sessions[0].Path != paths[3] {
		t.Fatalf("restart changed recent history: %+v", restored)
	}
	ordinary := treeFor(t, srv, "?scope=active&limit=1")
	if ordinary.Sessions[0].Path == paths[3] {
		t.Fatal("Recent changed the manually placed All order")
	}
}

func TestRemoteTreeForwardsRecentPagination(t *testing.T) {
	noLinger(t)
	asked := make(chan string, 1)
	far := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked <- r.URL.RequestURI()
		writeJSON(w, []treeWorkspace{})
	}))
	defer far.Close()
	front := bookServer(t, &stubAttacher{attach: func(host, workspace string) (RemoteEndpoint, func(), error) {
		return RemoteEndpoint{Host: host, Workspace: workspace, Addr: far.Listener.Addr().String(), Token: "t"}, func() {}, nil
	}})
	bookPost(t, front, "/remotes", `{"name":"gpu-box","host":"10.0.0.4"}`)
	query := "?order=recent&scope=active&limit=50&offset=350&hideLegacy=1&q=release"
	_ = hubGet[[]treeWorkspace](t, front, "/remotes/gpu-box/tree"+query)
	if got := <-asked; got != "/tree"+query {
		t.Fatalf("far request = %s", got)
	}
}

func TestRecentWindowIgnoresPerProjectSidebarLimits(t *testing.T) {
	for _, limit := range []int{0, 50} {
		window := treeWindow{recent: true, limit: limit, offset: 350, limits: map[string]int{"root": 1}}
		want := 0
		if limit > 0 {
			want = 400
		}
		if got := window.limitFor("root"); got != want {
			t.Fatalf("limit %d: candidate prefix %d, want %d", limit, got, want)
		}
	}
}

func TestRecentTreeDoesNotDiagnoseIntentionallyUnlistedOpenRowsAsOrphans(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	root := testenv.TempDir(t)
	h := NewHub(HubOptions{})
	rt := hubRuntime(t, h, root)
	paths := seedAged(t, root, 2)
	rt.Server.Controller().SetSessionPath(paths[0])
	srv := httptest.NewServer(operatorHandler(h))
	defer srv.Close()
	page := treeFor(t, srv, "?order=recent&scope=active&limit=1")
	if len(page.Sessions) != 1 || page.Sessions[0].Path != paths[1] {
		t.Fatalf("recent page must not inject an older open row: %+v", page)
	}
	if _, logged := h.orphanLogged.Load(rt.ID + "|" + sessionstore.CanonicalSessionPath(paths[0])); logged {
		t.Fatal("a valid open session outside a recent page was diagnosed as orphaned")
	}
}

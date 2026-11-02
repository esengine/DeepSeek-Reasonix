package serve

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/state/sessionstore"
	"reasonix/internal/state/store"
)

// seedAged writes n conversations whose age rises with their number, so
// session-000 is the oldest and the newest is session-(n-1).
func seedAged(t *testing.T, root string, n int) []string {
	t.Helper()
	base := time.Now().Add(-time.Duration(n) * time.Hour)
	paths := make([]string, n)
	for i := range n {
		p := filepath.Join(SessionDirFor(root), fmt.Sprintf("session-%03d.jsonl", i))
		writeSessionAt(t, p)
		at := base.Add(time.Duration(i) * time.Hour)
		// The listing reads activity from the branch meta before any file time.
		if err := sessionstore.UpdateBranchMeta(p, false, func(m *sessionstore.BranchMeta) error {
			m.UpdatedAt = at
			return nil
		}); err != nil {
			t.Fatalf("age session meta: %v", err)
		}
		for _, f := range []string{p, p + ".meta", store.SessionEventLog(p)} {
			if _, err := os.Stat(f); f != "" && err == nil {
				_ = os.Chtimes(f, at, at)
			}
		}
		paths[i] = p
	}
	return paths
}

func treeFor(t *testing.T, srv *httptest.Server, query string) treeWorkspace {
	t.Helper()
	for _, ws := range hubGet[[]treeWorkspace](t, srv, "/tree"+query) {
		return ws
	}
	t.Fatal("no workspace in tree")
	return treeWorkspace{}
}

func TestTreeBuildsAWindowAndCountsTheWholeWorkspace(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	root := testenv.TempDir(t)
	h := NewHub(HubOptions{})
	hubRuntime(t, h, root)
	srv := httptest.NewServer(operatorHandler(h))
	defer srv.Close()
	paths := seedAged(t, root, 132)
	if err := sessionstore.SetSessionLineageArchived(paths[3], true); err != nil {
		t.Fatal(err)
	}

	first := treeFor(t, srv, "")
	if len(first.Sessions) != defaultTreeWindow || first.Total != 132 || first.Archived != 1 {
		t.Fatalf("default window: %d rows, total %d, archived %d; want %d rows of 132 with 1 archived",
			len(first.Sessions), first.Total, first.Archived, defaultTreeWindow)
	}
	if got := first.Sessions[0].Name; got != "session-131" {
		t.Fatalf("newest first: got %s", got)
	}

	wider := treeFor(t, srv, "?limit=80")
	if len(wider.Sessions) != 80 || wider.Total != 132 {
		t.Fatalf("limit=80: %d rows of %d", len(wider.Sessions), wider.Total)
	}
	for i, row := range first.Sessions {
		if wider.Sessions[i].Path != row.Path {
			t.Fatalf("row %d moved when the window grew: %s then %s", i, row.Name, wider.Sessions[i].Name)
		}
	}

	all := treeFor(t, srv, "?limit=0")
	if len(all.Sessions) != 132 || all.Total != 132 {
		t.Fatalf("limit=0: %d rows of %d", len(all.Sessions), all.Total)
	}

	per := treeFor(t, srv, "?limits="+url.QueryEscape(fmt.Sprintf(`{%q:70}`, root)))
	if len(per.Sessions) != 70 {
		t.Fatalf("per-workspace limit: %d rows", len(per.Sessions))
	}
}

func TestTreeRefusesAnUnreadableWindow(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	h := NewHub(HubOptions{})
	hubRuntime(t, h, testenv.TempDir(t))
	srv := httptest.NewServer(operatorHandler(h))
	defer srv.Close()
	for _, q := range []string{"?limit=-1", "?limit=many", "?limits=nope", `?limits=%7B%22a%22%3A-2%7D`, "?order=unknown", "?offset=1", "?order=recent&offset=-1", "?order=recent&offset=9223372036854775807"} {
		resp, err := http.Get(srv.URL + "/tree" + q)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", q, resp.StatusCode)
		}
	}
}

func TestTreeTotalFoldsRecoveryCopiesAndFollowsDeletes(t *testing.T) {
	root, _, _ := seedConflictChain(t)
	h := NewHub(HubOptions{})
	hubRuntime(t, h, root)
	srv := httptest.NewServer(operatorHandler(h))
	defer srv.Close()
	extra := filepath.Join(SessionDirFor(root), "20260822-000000-deepseek.jsonl")
	writeSessionAt(t, extra)

	full := treeFor(t, srv, "?limit=0")
	one := treeFor(t, srv, "?limit=1")
	if one.Total != full.Total || len(one.Sessions) != 1 {
		t.Fatalf("a one-row window counted %d (full %d) with %d rows", one.Total, full.Total, len(one.Sessions))
	}
	if full.Total != len(full.Sessions) {
		t.Fatalf("total %d disagrees with the %d rows drawn: recovery copies must not count twice", full.Total, len(full.Sessions))
	}

	post := postJSON(t, srv.URL+"/tree/sessions/remove", map[string]string{"path": extra})
	if post.StatusCode != http.StatusNoContent {
		t.Fatalf("remove: %d", post.StatusCode)
	}
	after := treeFor(t, srv, "?limit=1")
	if after.Total != full.Total-1 {
		t.Fatalf("total %d after a delete, want %d", after.Total, full.Total-1)
	}
}

func TestArchivedScopeWindowsTheArchivedRowsThemselves(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	root := testenv.TempDir(t)
	h := NewHub(HubOptions{})
	hubRuntime(t, h, root)
	srv := httptest.NewServer(operatorHandler(h))
	defer srv.Close()
	paths := seedAged(t, root, 132)
	// The two oldest conversations are archived, far outside the default window.
	for _, p := range paths[:2] {
		if err := sessionstore.SetSessionLineageArchived(p, true); err != nil {
			t.Fatal(err)
		}
	}
	ws := treeFor(t, srv, "?scope=archived")
	if ws.Archived != 2 || len(ws.Sessions) != 2 || ws.Remaining != 0 {
		t.Fatalf("archived scope: %d rows, archived %d, remaining %d; the old archived rows must be reachable", len(ws.Sessions), ws.Archived, ws.Remaining)
	}
	for _, row := range ws.Sessions {
		if !row.Archived {
			t.Fatalf("an active row %s came back under the archived scope", row.Name)
		}
	}
	active := treeFor(t, srv, "?scope=active")
	if len(active.Sessions) != defaultTreeWindow || active.Remaining != 130-defaultTreeWindow {
		t.Fatalf("active scope: %d rows, remaining %d; want %d rows and %d remaining (archived rows are not counted as more)", len(active.Sessions), active.Remaining, defaultTreeWindow, 130-defaultTreeWindow)
	}
}

func TestAPinnedConversationIsAlwaysBuiltSoThePinnedCountCannotMove(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	root := testenv.TempDir(t)
	h := NewHub(HubOptions{})
	hubRuntime(t, h, root)
	srv := httptest.NewServer(operatorHandler(h))
	defer srv.Close()
	paths := seedAged(t, root, 132)
	if err := sessionstore.SetSessionPinned(paths[1], true); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{"", "?scope=archived", "?scope=active&q=nothing-matches", "?limit=5"} {
		ws := treeFor(t, srv, query)
		var found bool
		for _, row := range ws.Sessions {
			found = found || (row.Path == paths[1] && row.Pinned)
		}
		if !found {
			t.Errorf("query %q left the pinned old conversation out of the tree", query)
		}
	}
}

func TestTreeQueryFiltersByTitleOrNameAndCountsWhatIsLeft(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	root := testenv.TempDir(t)
	h := NewHub(HubOptions{})
	hubRuntime(t, h, root)
	srv := httptest.NewServer(operatorHandler(h))
	defer srv.Close()
	paths := seedAged(t, root, 132)
	for i, p := range paths {
		title := fmt.Sprintf("Notes %03d", i)
		if i >= 100 && i < 110 {
			title = fmt.Sprintf("Release Plan %d", i)
		}
		if err := sessionstore.RenameSession(p, title); err != nil {
			t.Fatal(err)
		}
	}

	hits := treeFor(t, srv, "?q=RELEASE%20plan&limit=4")
	if len(hits.Sessions) != 4 || hits.Remaining != 6 || hits.Total != 132 {
		t.Fatalf("q=RELEASE plan limit=4: %d rows, remaining %d, total %d; want 4 rows, 6 more of 10 matches, total untouched", len(hits.Sessions), hits.Remaining, hits.Total)
	}
	none := treeFor(t, srv, "?q=zzz-no-such")
	if len(none.Sessions) != 0 || none.Remaining != 0 {
		t.Fatalf("a query nothing matches: %d rows, remaining %d", len(none.Sessions), none.Remaining)
	}
	name := treeFor(t, srv, "?q="+url.QueryEscape(filepath.Base(root)))
	if len(name.Sessions) != defaultTreeWindow {
		t.Fatalf("a query naming the workspace keeps its rows: got %d", len(name.Sessions))
	}
}

func TestTreeRefusesAnUnknownScope(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	h := NewHub(HubOptions{})
	hubRuntime(t, h, testenv.TempDir(t))
	srv := httptest.NewServer(operatorHandler(h))
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/tree?scope=everything")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("scope=everything -> %d, want 400", resp.StatusCode)
	}
}

func TestTreeServesAnOversizedWindowAtTheCap(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	h := NewHub(HubOptions{})
	root := testenv.TempDir(t)
	hubRuntime(t, h, root)
	seedAged(t, root, maxTreeWindow+10)
	srv := httptest.NewServer(operatorHandler(h))
	defer srv.Close()
	for _, query := range []string{"?limit=2000000000", "?limit=9223372036854775807"} {
		got := treeFor(t, srv, query)
		if len(got.Sessions) != maxTreeWindow || got.Remaining != 10 || got.Total != maxTreeWindow+10 {
			t.Errorf("%s: %d rows, remaining %d, total %d; want %d rows, 10 remaining, total %d", query, len(got.Sessions), got.Remaining, got.Total, maxTreeWindow, maxTreeWindow+10)
		}
	}
}

func TestParseTreeWindowClampsEveryBound(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/tree?limit=999999&limits=%7B%22a%22%3A999999%2C%22b%22%3A0%7D", nil)
	window, ok := parseTreeWindow(httptest.NewRecorder(), req)
	if !ok {
		t.Fatal("refused")
	}
	if window.limit != maxTreeWindow || window.limits["a"] != maxTreeWindow || window.limits["b"] != 0 {
		t.Fatalf("limit %d limits %v; want %d, a clamped, b still 0", window.limit, window.limits, maxTreeWindow)
	}
}

func TestTreeRowsCarryTheActivityTimeTheListSortsBy(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	root := testenv.TempDir(t)
	h := NewHub(HubOptions{})
	hubRuntime(t, h, root)
	srv := httptest.NewServer(operatorHandler(h))
	defer srv.Close()
	seedAged(t, root, 3)

	listed, err := sessionstore.ListSessions(SessionDirFor(root))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int64{}
	for _, si := range listed {
		want[si.Path] = si.LastActivityAt.UnixMilli()
	}
	rows := treeFor(t, srv, "").Sessions
	if len(rows) != 3 {
		t.Fatalf("%d rows, want 3", len(rows))
	}
	for _, row := range rows {
		if row.LastActiveAt == 0 || row.LastActiveAt != want[row.Path] {
			t.Fatalf("%s lastActiveAt = %d, want the list's own %d", row.Name, row.LastActiveAt, want[row.Path])
		}
	}
}

package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/session"
)

// newV4SessionServe publishes one v4 store and returns a Serve whose title
// cache lives in that store root, matching the CLI's Serve wiring.
func newV4SessionServe(t *testing.T) (*Server, *session.Service, string) {
	t.Helper()
	// The CLI hands Serve the legacy transcript catalog as SessionDir and the
	// service the sibling sessions-v4 store; titles are cached in the store.
	legacyDir := filepath.Join(t.TempDir(), "sessions")
	root := filepath.Join(filepath.Dir(legacyDir), "sessions-v4")
	service, err := session.NewService("local", session.NewFilesystemPersistence(root))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := service.CloseAll(t.Context()); err != nil {
			t.Errorf("close sessions: %v", err)
		}
	})
	exec := agent.New(nil, nil, agent.NewSession("system"), agent.Options{}, event.Discard)
	ctrl := control.New(control.Options{Executor: exec, SessionDir: legacyDir, SessionService: service})
	t.Cleanup(ctrl.Close)
	if _, err := ctrl.BindFreshSession(t.Context(), "current"); err != nil {
		t.Fatal(err)
	}
	return New(ctrl, NewBroadcaster(), config.ServeConfig{}), service, root
}

func addV4UserTurn(t *testing.T, svc *session.Service, id, content string) {
	t.Helper()
	handle, err := svc.Create(t.Context(), session.CreateOptions{SessionID: id})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(map[string]any{
		"message": provider.Message{ID: id + "-user", Role: provider.RoleUser, Content: content, Origin: provider.MessageOriginUser},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := handle.Session().AppendBatch(t.Context(), id+"-turn",
		[]session.Event{{Kind: "message/complete", Payload: payload}}); err != nil {
		t.Fatal(err)
	}
	// Flush publishes the catalog cache the listing reads.
	if _, err := handle.Session().Flush(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := svc.Close(t.Context(), handle.Ref()); err != nil {
		t.Fatal(err)
	}
}

func listSessionRows(t *testing.T, srv *Server) []sessionListEntry {
	t.Helper()
	rec := httptest.NewRecorder()
	srv.sessions(rec, httptest.NewRequest(http.MethodGet, "/sessions", nil))
	var rows []sessionListEntry
	if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
		t.Fatalf("decode /sessions: %v (%s)", err, rec.Body.String())
	}
	return rows
}

func rowFor(rows []sessionListEntry, sessionID string) (sessionListEntry, bool) {
	for _, row := range rows {
		if row.SessionID == sessionID {
			return row, true
		}
	}
	return sessionListEntry{}, false
}

// TestSessionsReturnsStoredV4Title reproduces issue #10533: the stored-session
// branch must expose the title cached in the store's .session-titles.json
// instead of only a hex session id, and must fall back to the first authored
// message (never the id) when no title exists yet.
func TestSessionsReturnsStoredV4Title(t *testing.T) {
	srv, svc, root := newV4SessionServe(t)
	addV4UserTurn(t, svc, "titled", "整理一下 AI 剧场的目录")
	addV4UserTurn(t, svc, "untitled", "second")

	newTitleCache(root).put("titled", "AI剧场目录整理", "整理一下 AI 剧场的目录", 0)

	rows := listSessionRows(t, srv)
	titled, ok := rowFor(rows, "titled")
	if !ok {
		t.Fatalf("titled session missing from rows %+v", rows)
	}
	if titled.Title != "AI剧场目录整理" {
		t.Fatalf("title = %q, want the stored v4 title", titled.Title)
	}
	// The Server has no title provider, so a stored hit is the only source: an
	// untitled session falls back to its first message, never to the hex id.
	untitled, ok := rowFor(rows, "untitled")
	if !ok {
		t.Fatalf("untitled session missing from rows %+v", rows)
	}
	if untitled.Title != "second" {
		t.Fatalf("untitled session title = %q, want the first-message preview", untitled.Title)
	}
}

// TestSessionsOrdersV4SessionsByRecency pins the durable log mtime, which is
// what the legacy branch reports and what the sidebar orders by.
func TestSessionsOrdersV4SessionsByRecency(t *testing.T) {
	srv, svc, _ := newV4SessionServe(t)
	addV4UserTurn(t, svc, "older", "first session")
	time.Sleep(10 * time.Millisecond)
	addV4UserTurn(t, svc, "newer", "second session")

	rows := listSessionRows(t, srv)
	older, ok := rowFor(rows, "older")
	if !ok {
		t.Fatalf("older session missing from rows %+v", rows)
	}
	newer, ok := rowFor(rows, "newer")
	if !ok {
		t.Fatalf("newer session missing from rows %+v", rows)
	}
	if newer.MtimeMilli <= older.MtimeMilli {
		t.Fatalf("newer mtime %d <= older mtime %d", newer.MtimeMilli, older.MtimeMilli)
	}
	for i := 1; i < len(rows); i++ {
		if rows[i-1].MtimeMilli < rows[i].MtimeMilli {
			t.Fatalf("rows are not newest-first: %+v", rows)
		}
	}
}

// TestSessionsGeneratesAndCachesV4Title covers the cold path: a stored v4
// session with no cached title gets one generated from its first message, and
// the generated title is persisted under the session id for later polls.
func TestSessionsGeneratesAndCachesV4Title(t *testing.T) {
	srv, svc, root := newV4SessionServe(t)
	addV4UserTurn(t, svc, "cold", "explain the session store")
	prov := &recordingTitleProvider{}
	srv.titleProv = prov

	rows := listSessionRows(t, srv)
	row, ok := rowFor(rows, "cold")
	if !ok {
		t.Fatalf("cold session missing from rows %+v", rows)
	}
	if row.Title != "explain the session store" {
		t.Fatalf("generated title = %q", row.Title)
	}
	if len(prov.requests) != 1 {
		t.Fatalf("title requests = %d, want 1", len(prov.requests))
	}
	if got, ok := newTitleCache(root).get("cold", "explain the session store", 0); !ok || got != row.Title {
		t.Fatalf("persisted cache entry = %q,%v; want %q,true", got, ok, row.Title)
	}

	// A warmed cache must not re-request the title on the next poll.
	if _, again := listSessionRows(t, srv), len(prov.requests); again != 1 {
		t.Fatalf("title requests after warm poll = %d, want 1", again)
	}
}

// TestSessionsPrefersEventLogTitleOverGeneratedTitle keeps an explicit rename
// (or the agent's own title tool) authoritative over the generated cache.
func TestSessionsPrefersEventLogTitleOverGeneratedTitle(t *testing.T) {
	srv, svc, root := newV4SessionServe(t)
	addV4UserTurn(t, svc, "renamed", "rename me in the sidebar")
	newTitleCache(root).put("renamed", "generated title", "rename me in the sidebar", 0)

	ref := session.SessionRef{HostID: "local", SessionID: "renamed"}
	if err := svc.SetTitle(t.Context(), ref, "手动重命名"); err != nil {
		t.Fatal(err)
	}

	rows := listSessionRows(t, srv)
	row, ok := rowFor(rows, "renamed")
	if !ok {
		t.Fatalf("renamed session missing from rows %+v", rows)
	}
	if row.Title != "手动重命名" {
		t.Fatalf("title = %q, want the durable event-log title", row.Title)
	}
}

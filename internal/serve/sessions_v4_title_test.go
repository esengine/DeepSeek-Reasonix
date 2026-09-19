package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/session"
)

// serveFixture mirrors the CLI's Serve wiring: SessionDir is the legacy
// transcript catalog, the session service owns the sibling sessions-v4 store,
// and generated titles are cached inside that store root.
type serveFixture struct {
	srv       *Server
	service   *session.Service
	store     string
	legacyDir string
}

func newServeFixture(t *testing.T) serveFixture {
	t.Helper()
	legacyDir := filepath.Join(t.TempDir(), "sessions")
	store := filepath.Join(filepath.Dir(legacyDir), "sessions-v4")
	service, err := session.NewService("local", session.NewFilesystemPersistence(store))
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
	return serveFixture{
		srv: New(ctrl, NewBroadcaster(), config.ServeConfig{}), service: service,
		store: store, legacyDir: legacyDir,
	}
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
	f := newServeFixture(t)
	addV4UserTurn(t, f.service, "titled", "整理一下 AI 剧场的目录")
	addV4UserTurn(t, f.service, "untitled", "second")

	newTitleCache(f.store).put("titled", "AI剧场目录整理", "整理一下 AI 剧场的目录", 0)

	rows := listSessionRows(t, f.srv)
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
	f := newServeFixture(t)
	addV4UserTurn(t, f.service, "older", "first session")
	time.Sleep(10 * time.Millisecond)
	addV4UserTurn(t, f.service, "newer", "second session")

	rows := listSessionRows(t, f.srv)
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
	f := newServeFixture(t)
	addV4UserTurn(t, f.service, "cold", "explain the session store")
	prov := &recordingTitleProvider{}
	f.srv.titleProv = prov

	rows := listSessionRows(t, f.srv)
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
	if got, ok := newTitleCache(f.store).get("cold", "explain the session store", 0); !ok || got != row.Title {
		t.Fatalf("persisted cache entry = %q,%v; want %q,true", got, ok, row.Title)
	}

	// A warmed cache must not re-request the title on the next poll.
	if _, again := listSessionRows(t, f.srv), len(prov.requests); again != 1 {
		t.Fatalf("title requests after warm poll = %d, want 1", again)
	}
}

// TestSessionsPrefersEventLogTitleOverGeneratedTitle keeps an explicit rename
// (or the agent's own title tool) authoritative over the generated cache.
func TestSessionsPrefersEventLogTitleOverGeneratedTitle(t *testing.T) {
	f := newServeFixture(t)
	addV4UserTurn(t, f.service, "renamed", "rename me in the sidebar")
	newTitleCache(f.store).put("renamed", "generated title", "rename me in the sidebar", 0)

	ref := session.SessionRef{HostID: "local", SessionID: "renamed"}
	if err := f.service.SetTitle(t.Context(), ref, "手动重命名"); err != nil {
		t.Fatal(err)
	}

	rows := listSessionRows(t, f.srv)
	row, ok := rowFor(rows, "renamed")
	if !ok {
		t.Fatalf("renamed session missing from rows %+v", rows)
	}
	if row.Title != "手动重命名" {
		t.Fatalf("title = %q, want the durable event-log title", row.Title)
	}
}

// TestSessionsKeepsLegacyTitlesAfterCacheMove guards the legacy .jsonl branch
// across the cache relocation: generated titles for transcript sessions are
// stored under the store root too, or an upgrade would silently regenerate
// every legacy title and re-spend provider requests on the first poll.
func TestSessionsKeepsLegacyTitlesAfterCacheMove(t *testing.T) {
	f := newServeFixture(t)
	if err := os.MkdirAll(f.legacyDir, 0o700); err != nil {
		t.Fatal(err)
	}
	const name = "20260901-login-loop.jsonl"
	legacy := agent.NewSession("system")
	legacy.Add(provider.Message{Role: provider.RoleUser, Content: "debug the login loop"})
	if err := legacy.Save(filepath.Join(f.legacyDir, name)); err != nil {
		t.Fatal(err)
	}
	newTitleCache(f.store).put(name, "Login loop debug", "debug the login loop", 0)

	rows := listSessionRows(t, f.srv)
	for _, row := range rows {
		if row.Name != strings.TrimSuffix(name, ".jsonl") {
			continue
		}
		if row.Title != "Login loop debug" {
			t.Fatalf("legacy title = %q, want the stored title", row.Title)
		}
		return
	}
	t.Fatalf("legacy row %q missing from rows %+v", name, rows)
}

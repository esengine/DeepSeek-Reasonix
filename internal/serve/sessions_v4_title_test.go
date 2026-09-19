package serve

import (
	"context"
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

// TestGeneratedTitleBecomesDurable proves the title a listing generates is
// written through to the session's own event log. A second, independent service
// over the same store - the shape of a different transport, or this one after a
// restart - must read it without generating anything, and it must survive with
// the local title cache deleted.
func TestGeneratedTitleBecomesDurable(t *testing.T) {
	f := newServeFixture(t)
	addV4UserTurn(t, f.service, "durable", "explain the session store")
	prov := &recordingTitleProvider{}
	f.srv.titleProv = prov

	rows := listSessionRows(t, f.srv)
	row, ok := rowFor(rows, "durable")
	if !ok {
		t.Fatalf("session missing from rows %+v", rows)
	}
	if row.Title != "explain the session store" {
		t.Fatalf("listed title = %q", row.Title)
	}

	// The disposable cache is gone; only the durable log can answer now.
	if err := os.Remove(filepath.Join(f.store, ".session-titles.json")); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	reader, err := session.NewService("local", session.NewFilesystemPersistence(f.store))
	if err != nil {
		t.Fatal(err)
	}
	defer reader.CloseAll(t.Context())
	info, err := reader.Query().Stat(t.Context(), session.SessionRef{HostID: "local", SessionID: "durable"})
	if err != nil {
		t.Fatal(err)
	}
	if info.Title != "explain the session store" {
		t.Fatalf("durable title = %q, want the generated title in the event log", info.Title)
	}
	if info.TitleSequence == 0 {
		t.Fatal("durable title has no revision, so a later rename cannot fence it")
	}
}

// TestGeneratedTitleWriteRespectsConcurrentRename fences the write-through with
// the revision the listing read: if the session was renamed after that read, the
// generated title must be dropped rather than overwriting the rename.
func TestGeneratedTitleWriteRespectsConcurrentRename(t *testing.T) {
	f := newServeFixture(t)
	addV4UserTurn(t, f.service, "raced", "explain the session store")
	prov := &recordingTitleProvider{}
	f.srv.titleProv = prov

	// The listing read revision N, then a manual rename landed, advancing it.
	stale, err := f.service.Query().Stat(t.Context(), session.SessionRef{HostID: "local", SessionID: "raced"})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.service.SetTitle(t.Context(), stale.Ref, "手动重命名"); err != nil {
		t.Fatal(err)
	}
	f.srv.persistGeneratedTitle(t.Context(), f.service, stale, "explain the session store")

	rows := listSessionRows(t, f.srv)
	row, ok := rowFor(rows, "raced")
	if !ok {
		t.Fatalf("session missing from rows %+v", rows)
	}
	if row.Title != "手动重命名" {
		t.Fatalf("title = %q, want the rename to survive the racing generation", row.Title)
	}
}

// TestGeneratedTitleWriteFailureKeepsListing guards the best-effort contract: a
// write-through that loses the race for the writer lease must not fail the poll,
// and the generated title still names the session from the local cache.
func TestGeneratedTitleWriteFailureKeepsListing(t *testing.T) {
	f := newServeFixture(t)
	addV4UserTurn(t, f.service, "busy", "explain the session store")
	prov := &recordingTitleProvider{}
	f.srv.titleProv = prov

	// A second service holds the session's writer lease, so the durable write
	// cannot land while the listing is served.
	holder, err := session.NewService("local", session.NewFilesystemPersistence(f.store))
	if err != nil {
		t.Fatal(err)
	}
	binding, err := holder.Open(t.Context(), session.SessionRef{HostID: "local", SessionID: "busy"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := binding.Release(context.Background()); err != nil {
			t.Errorf("release holder binding: %v", err)
		}
		if err := holder.CloseAll(context.Background()); err != nil {
			t.Errorf("close holder service: %v", err)
		}
	})

	rows := listSessionRows(t, f.srv)
	row, ok := rowFor(rows, "busy")
	if !ok {
		t.Fatalf("session missing from rows %+v", rows)
	}
	if row.Title != "explain the session store" {
		t.Fatalf("title = %q, want the generated title despite the failed write", row.Title)
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

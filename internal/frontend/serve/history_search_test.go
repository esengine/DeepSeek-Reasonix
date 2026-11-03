package serve

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/provider"
	"reasonix/internal/state/history"
	"reasonix/internal/state/sessionstore"
	"reasonix/internal/state/store"
)

func TestHistorySearchRefusesAnEmptyQueryWithItsCode(t *testing.T) {

	mux := http.NewServeMux()
	mux.HandleFunc("GET /history/search", historySearch)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/history/search?q=%20", nil))
	var reason Reason
	_ = json.Unmarshal(rec.Body.Bytes(), &reason)
	if rec.Code != http.StatusBadRequest || reason.Code != "history.query_required" {
		t.Fatalf("status %d code %q, want 400 history.query_required", rec.Code, reason.Code)
	}
}

func TestHistorySearchPreservesCatalogFailure(t *testing.T) {
	if err := history.CloseSharedCatalog(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Setenv("REASONIX_CACHE_HOME", filepath.Join(testenv.TempDir(t), "cache"))
	closeSharedCatalogsOnCleanup(t)
	searcher := history.NewIndexedSearcher(history.Options{SessionDir: testenv.TempDir(t)})
	waitFixtureCatalog(t, searcher)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	rec := httptest.NewRecorder()
	historySearch(rec, httptest.NewRequest(http.MethodGet, "/history/search?q=zebra", nil).WithContext(ctx))
	var reason Reason
	if err := json.Unmarshal(rec.Body.Bytes(), &reason); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusInternalServerError || reason.Code != "history.search_failed" {
		t.Fatalf("status %d code %q, want 500 history.search_failed", rec.Code, reason.Code)
	}
}

func TestHistorySearchAnswersAnEmptyResultAndAnOpeningIndexWithoutAnIndexYet(t *testing.T) {
	if err := history.CloseSharedCatalog(t.Context()); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /history/search", historySearch)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/history/search?q=zebra", nil))
	var got historySearchResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK || got.Hits == nil || len(got.Hits) != 0 {
		t.Fatalf("status %d body %q, want 200 and an empty JSON hit list", rec.Code, rec.Body.String())
	}
	if !got.Index.Partial || got.Index.Reason != history.SearchIndexBuilding {
		t.Fatalf("an index that has not opened must say it is still building, got %+v", got.Index)
	}
}

// A transcript the index knows but cannot read is excluded from the query, so
// the search answers with no hits. That is not the same fact as a query that
// matched nothing, and the response has to carry the difference: the catalog
// already recorded the failure, and the rail cannot phrase what it never gets.
func TestHistorySearchReportsASessionTheIndexCouldNotRead(t *testing.T) {
	requireDeniableReads(t)
	useTemporaryHistoryCatalog(t)
	dir := testenv.TempDir(t)
	path := saveHistoryFixture(t, dir, "unreadable.jsonl",
		provider.Message{Role: provider.RoleUser, Content: "which zebra migration order did we choose?"},
		provider.Message{Role: provider.RoleAssistant, Content: "the phoenix-first zebra order"})
	waitHistorySearch(t, "zebra migration", 1)

	// The event log becomes unreadable while the row's identity still matches:
	// the index cannot serve the session but does not yet know it.
	logPath := store.SessionEventLog(path)
	if err := os.Chmod(logPath, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(logPath, 0o600) })

	rec := httptest.NewRecorder()
	historySearch(rec, httptest.NewRequest(http.MethodGet, "/history/search?q=zebra", nil))
	var got historySearchResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %q, want 200", rec.Code, rec.Body.String())
	}
	if len(got.Hits) != 0 {
		t.Fatalf("the only matching source is unreadable, want no hits, got %+v", got.Hits)
	}
	if !got.Index.Partial || got.Index.Reason != history.SearchIndexFailed {
		t.Fatalf("a search over a source the index could not read must say so, got %+v", got.Index)
	}
	if got.Index.Failed == 0 || got.Index.Indexed >= got.Index.Total {
		t.Fatalf("response does not carry the failure the catalog recorded: %+v", got.Index)
	}
}

// Control: a projection that answered for every source says nothing about the
// index — not for a matching query, and not for a query that matched nothing.
func TestHistorySearchSaysNothingExtraWhenTheIndexAnsweredForEverySource(t *testing.T) {
	useTemporaryHistoryCatalog(t)
	dir := testenv.TempDir(t)
	saveHistoryFixture(t, dir, "readable.jsonl",
		provider.Message{Role: provider.RoleUser, Content: "which zebra migration order did we choose?"},
		provider.Message{Role: provider.RoleAssistant, Content: "the phoenix-first zebra order"})
	waitHistorySearch(t, "zebra migration", 1)

	for _, query := range []string{"zebra", "quux nonexistent"} {
		deadline := time.Now().Add(20 * time.Second)
		for {
			rec := httptest.NewRecorder()
			historySearch(rec, httptest.NewRequest(http.MethodGet, "/history/search?q="+url.QueryEscape(query), nil))
			var got historySearchResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if rec.Code != http.StatusOK {
				t.Fatalf("%q: status %d body %q, want 200", query, rec.Code, rec.Body.String())
			}
			if got.Index.Failed != 0 || got.Index.Reason == history.SearchIndexFailed {
				t.Fatalf("%q: a readable source was reported as unreadable: %+v", query, got.Index)
			}
			settled := got.Index.State == "ready" && got.Index.Total > 0 && got.Index.Indexed == got.Index.Total
			if settled && history.SearchIndexStatus().Pending == 0 {
				if got.Index.Partial || got.Index.Reason != "" {
					t.Fatalf("%q: a complete projection raised an incomplete-index signal: %+v", query, got.Index)
				}
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("%q: the projection never settled: %+v", query, got.Index)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
}

// requireDeniableReads skips a permission fixture on hosts where chmod cannot
// deny a read: Windows maps 000 to the read-only attribute, and root ignores
// the mode bits entirely.
func requireDeniableReads(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("chmod 000 cannot deny a read on this platform/user")
	}
}

// useTemporaryHistoryCatalog points the shared catalog at this test's own cache
// and closes it afterwards, so a fixture never reads or writes the real one.
func useTemporaryHistoryCatalog(t *testing.T) {
	t.Helper()
	if err := history.CloseSharedCatalog(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Setenv("REASONIX_CACHE_HOME", filepath.Join(testenv.TempDir(t), "cache"))
	closeSharedCatalogsOnCleanup(t)
}

// saveHistoryFixture writes one session and registers its directory, which is
// what opens the shared catalog in production too.
func saveHistoryFixture(t *testing.T, dir, name string, messages ...provider.Message) string {
	t.Helper()
	path := filepath.Join(dir, name)
	session := sessionstore.NewSession("sys")
	for _, message := range messages {
		session.Add(message)
	}
	if err := session.Save(path); err != nil {
		t.Fatal(err)
	}
	waitFixtureCatalog(t, history.NewIndexedSearcher(history.Options{SessionDir: dir, GlobalSessionDir: dir}))
	return path
}

// waitHistorySearch waits for the background scan to surface the fixture.
func waitHistorySearch(t *testing.T, query string, want int) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		hits, err := history.SearchSessions(t.Context(), query, 10)
		if err == nil && len(hits) == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("index never surfaced %d session(s) for %q: %v, got %d", want, query, err, len(hits))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

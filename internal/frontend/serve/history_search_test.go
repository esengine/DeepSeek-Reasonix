package serve

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/state/history"
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

func TestHistorySearchAnswersAnArrayEvenWithNoIndexYet(t *testing.T) {
	if err := history.CloseSharedCatalog(t.Context()); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /history/search", historySearch)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/history/search?q=zebra", nil))
	var hits []historySearchHit
	if err := json.Unmarshal(rec.Body.Bytes(), &hits); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK || hits == nil || len(hits) != 0 {
		t.Fatalf("status %d body %q, want 200 and an empty JSON array", rec.Code, rec.Body.String())
	}
}

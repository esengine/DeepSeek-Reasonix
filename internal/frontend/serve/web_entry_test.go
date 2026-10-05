package serve

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"reasonix/internal/contract/config"
	"reasonix/internal/session/control"
)

func TestServeIndexPageAndSessionDeepLink(t *testing.T) {
	bc := NewBroadcaster()
	ctrl := control.New(control.Options{Sink: bc})
	server := New(ctrl, bc, config.ServeConfig{})
	server.page = fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<title>studio</title>")}}

	// A deep link names a session, and the page is what opens it. Both answer
	// with the same shell and keep the path, so the page can read it.
	for _, path := range []string{"/", "/sessions/reserved-session"} {
		rec := httptest.NewRecorder()
		operatorHandler(server).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "<title>studio</title>") {
			t.Errorf("GET %s = %d %q, want the page shell", path, rec.Code, rec.Body.String())
		}
	}
}

// The built Studio page is the interface. A kernel that drew a second one at /
// shipped two, and the older one was the one a browser landed on.
func TestRootHandsTheVisitorToTheBuiltPage(t *testing.T) {
	bc := NewBroadcaster()
	srv := New(control.New(control.Options{Sink: bc}), bc, config.ServeConfig{})
	srv.page = fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<title>studio</title>")}}
	rec := httptest.NewRecorder()
	operatorHandler(srv).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "<title>studio</title>") {
		t.Fatalf("GET / = %d %q, want the page shell", rec.Code, rec.Body.String())
	}
}

// With nothing mounted the redirect would come straight back here, so the
// kernel says what is missing instead of looping a browser.
func TestRootSaysSoWhenNoPageIsBuilt(t *testing.T) {
	bc := NewBroadcaster()
	srv := New(control.New(control.Options{Sink: bc}), bc, config.ServeConfig{})
	rec := httptest.NewRecorder()
	operatorHandler(srv).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET / with no page = %d, want 404", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "page.not_built") {
		t.Fatalf("body = %q, want the code a frontend can act on", rec.Body.String())
	}
}

package websearch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func probeOf(t *testing.T, h http.HandlerFunc) SearxngProbe {
	t.Helper()
	srv := httptest.NewServer(h)
	defer srv.Close()
	got, err := ProbeSearxng(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestProbeNamesWhyAnInstanceDidNotAnswer(t *testing.T) {
	cases := []struct {
		name   string
		h      http.HandlerFunc
		ok     bool
		code   string
		status int
	}{
		{"answers", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"results":[{"title":"t","url":"https://example.com","content":"c"}]}`))
		}, true, "", 0},
		{"found nothing is still an instance that answers", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"results":[]}`))
		}, true, "", 0},
		{"json format not listed", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusForbidden) }, false, CodeSearxngJSONDisabled, 403},
		{"a page that is not json", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("<html>login</html>")) }, false, CodeSearxngNotSearxng, 0},
		{"json that is not searxng's", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"ok":true}`)) }, false, CodeSearxngNotSearxng, 0},
		{"rate limited", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTooManyRequests) }, false, CodeSearxngStatus, 429},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := probeOf(t, c.h)
			if got.OK != c.ok || got.Code != c.code || got.Status != c.status {
				t.Fatalf("probe = %+v, want ok=%v code=%q status=%d", got, c.ok, c.code, c.status)
			}
		})
	}
}

func TestProbeReportsAnAddressNothingListensOn(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	got, err := ProbeSearxng(context.Background(), url)
	if err != nil || got.OK || got.Code != CodeSearxngUnreachable {
		t.Fatalf("probe = %+v, %v", got, err)
	}
}

func TestProbeNeverContactsAHostTheRedirectNames(t *testing.T) {
	var elsewhere atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		elsewhere.Add(1)
		_, _ = w.Write([]byte(`{"results":[]}`))
	}))
	defer other.Close()
	got := probeOf(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/search", http.StatusFound)
	})
	if got.OK || got.Code != CodeSearxngRedirected || elsewhere.Load() != 0 {
		t.Fatalf("probe = %+v, other host contacted %d times", got, elsewhere.Load())
	}
}

func TestProbeRefusesAnAddressThatIsNotAnInstanceOrigin(t *testing.T) {
	if _, err := ProbeSearxng(context.Background(), "file:///etc/passwd"); err == nil {
		t.Fatal("a non-http address must be refused before any request")
	}
}

func TestSearchTellsTheModelWhyTheInstanceFailed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusForbidden) }))
	defer srv.Close()
	backend, err := NewSearxng(srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = backend.Search(context.Background(), "q")
	if got := failure(err).identity(); got != CodeSearxngJSONDisabled {
		t.Fatalf("identity = %q, want %q", got, CodeSearxngJSONDisabled)
	}
}

package responses

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIndependentSearchRejectsRedirects(t *testing.T) {
	forwarded := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/target" {
			forwarded = true
			return
		}
		w.Header().Set("Location", "/target")
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer srv.Close()
	p := New(Config{BaseURL: srv.URL, Model: "fixture", Extra: map[string]any{"reject_redirects": true}})
	resp, err := p.(*client).http.Post(srv.URL, "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if forwarded || resp.StatusCode != http.StatusTemporaryRedirect {
		t.Fatal("search request followed a redirect")
	}
}

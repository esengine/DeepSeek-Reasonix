package anthropic

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"reasonix/internal/contract/provider"
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
	client, err := newHTTPClient(provider.Config{Extra: map[string]any{"reject_redirects": true}})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Post(srv.URL, "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if forwarded || resp.StatusCode != http.StatusTemporaryRedirect {
		t.Fatal("search request followed a redirect")
	}
}

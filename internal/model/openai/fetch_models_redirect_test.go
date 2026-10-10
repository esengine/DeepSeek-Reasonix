package openai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestFetchModelListingWithoutAClientDoesNotFollowARedirectWithTheKey(t *testing.T) {
	var reached atomic.Int32
	elsewhere := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached.Add(1) }))
	defer elsewhere.Close()
	home := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, elsewhere.URL, http.StatusTemporaryRedirect)
	}))
	defer home.Close()
	if _, err := FetchModelListing(context.Background(), home.URL+"/v1", "secret", FetchModelsOptions{}); err == nil {
		t.Fatal("a cross-origin redirect was followed")
	}
	if n := reached.Load(); n != 0 {
		t.Fatalf("redirect target received %d request(s)", n)
	}
}

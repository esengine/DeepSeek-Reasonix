package typesafe

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestEvaluateWithoutAClientDoesNotFollowARedirectWithTheKey(t *testing.T) {
	var reached atomic.Int32
	elsewhere := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached.Add(1) }))
	defer elsewhere.Close()
	home := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, elsewhere.URL, http.StatusTemporaryRedirect)
	}))
	defer home.Close()
	_, err := (Client{BaseURL: home.URL, APIKey: func() string { return "secret" }}).Evaluate(context.Background(), Request{State: "s", Model: "m"})
	if err == nil {
		t.Fatal("a cross-origin redirect was followed")
	}
	if n := reached.Load(); n != 0 {
		t.Fatalf("redirect target received %d request(s)", n)
	}
}

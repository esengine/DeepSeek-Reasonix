package endpointclient

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"reasonix/internal/base/netclient"
	"reasonix/internal/safety/redirectguard"
)

func TestNewRefusesARedirectOffTheConfiguredOrigin(t *testing.T) {
	var reached atomic.Int32
	elsewhere := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached.Add(1) }))
	defer elsewhere.Close()
	home := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2":
		case "/moved":
			http.Redirect(w, r, "/v2", http.StatusTemporaryRedirect)
		default:
			http.Redirect(w, r, elsewhere.URL, http.StatusTemporaryRedirect)
		}
	}))
	defer home.Close()
	client, err := New(netclient.ProxySpec{Mode: netclient.ModeOff}, netclient.TransportOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := client.Post(home.URL, "application/json", strings.NewReader("{}")); !errors.Is(err, redirectguard.ErrRefused) {
		t.Fatalf("cross-origin redirect error = %v, want redirectguard.ErrRefused", err)
	}
	if n := reached.Load(); n != 0 {
		t.Fatalf("redirect target received %d request(s)", n)
	}
	resp, err := client.Post(home.URL+"/moved", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("same-origin redirect refused: %v", err)
	}
	resp.Body.Close()
}

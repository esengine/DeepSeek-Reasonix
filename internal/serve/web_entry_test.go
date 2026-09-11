package serve

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"reasonix/internal/config"
	"reasonix/internal/control"
)

func TestServeIndexPageAndSessionDeepLink(t *testing.T) {
	bc := NewBroadcaster()
	ctrl := control.New(control.Options{Sink: bc})
	srv := httptest.NewServer(New(ctrl, bc, config.ServeConfig{}).Handler())
	defer srv.Close()

	for _, path := range []string{"/", "/sessions/reserved-session"} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("GET %s status = %d", path, resp.StatusCode)
		}
		if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/html") {
			t.Errorf("GET %s content-type = %q, want text/html", path, ct)
		}
	}
}

func TestServeWebPagesBootstrapFragmentTokenBeforeRequests(t *testing.T) {
	for name, html := range map[string]string{
		"index":          string(indexHTML),
		"provider setup": string(providerSetupHTML),
	} {
		t.Run(name, func(t *testing.T) {
			for _, want := range []string{
				"new URLSearchParams(window.location.hash.slice(1))",
				"'/auth/token'",
				"window.history.replaceState",
				"window.fetch",
			} {
				if !strings.Contains(html, want) {
					t.Fatalf("page missing fragment-token bootstrap %q", want)
				}
			}
		})
	}
	if !strings.Contains(string(indexHTML), "__authReady.then(connectEvents)") {
		t.Fatal("serve index must delay SSE until fragment authentication completes")
	}
}

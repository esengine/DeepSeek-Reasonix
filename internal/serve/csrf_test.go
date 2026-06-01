package serve

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"reasonix/internal/control"
)

// TestServeRejectsNonJSONPost guards the CSRF defense: a state-changing POST that
// isn't application/json is refused, so a page the user visits can't drive the
// unauthenticated localhost server with a simple cross-origin POST (text/plain,
// no preflight). The same-origin frontend always sends JSON and is unaffected.
func TestServeRejectsNonJSONPost(t *testing.T) {
	got := make(chan string, 1)
	bc := NewBroadcaster()
	ctrl := control.New(control.Options{Runner: fakeRunner{got: got}, Sink: bc})
	srv := httptest.NewServer(New(ctrl, bc).Handler())
	defer srv.Close()

	for _, ct := range []string{"text/plain", "application/x-www-form-urlencoded", ""} {
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/submit", strings.NewReader(`{"input":"pwn"}`))
		if ct != "" {
			req.Header.Set("Content-Type", ct)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnsupportedMediaType {
			t.Errorf("Content-Type %q: status = %d, want 415", ct, resp.StatusCode)
		}
	}

	select {
	case in := <-got:
		t.Fatalf("a non-JSON POST reached the runner with %q — CSRF guard bypassed", in)
	default:
	}
}

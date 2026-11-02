package serve

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
)

// noLinger makes a request's link go back when the request ends, for tests that
// count the exchange rather than the hold.
func noLinger(t *testing.T) {
	t.Helper()
	old := remoteLinkLinger
	remoteLinkLinger = 0
	t.Cleanup(func() { remoteLinkLinger = old })
}

// A link taken for one request outlives it, so the next request on the same
// machine finds it instead of dialling again.
func TestARequestLinkIsHeldPastItsAnswer(t *testing.T) {
	old := remoteLinkLinger
	remoteLinkLinger = 150 * time.Millisecond
	t.Cleanup(func() { remoteLinkLinger = old })

	var released atomic.Int32
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	h := NewHub(HubOptions{Remote: &stubAttacher{attach: func(host, workspace string) (RemoteEndpoint, func(), error) {
		return RemoteEndpoint{Host: host, Workspace: workspace}, func() { released.Add(1) }, nil
	}}})
	front := httptest.NewServer(operatorHandler(h))
	t.Cleanup(front.Close)
	bookPost(t, front, "/remotes", `{"name":"gpu-box","host":"10.0.0.4"}`)

	_, release, err := h.remoteLink(httptest.NewRequest(http.MethodGet, "/", nil), "gpu-box")
	if err != nil {
		t.Fatal(err)
	}
	release()
	if got := released.Load(); got != 0 {
		t.Fatalf("link released %d time(s) at the end of the request, want it held", got)
	}
	deadline := time.Now().Add(5 * time.Second)
	for released.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := released.Load(); got != 1 {
		t.Fatalf("link released %d time(s) after the linger, want exactly once", got)
	}
}

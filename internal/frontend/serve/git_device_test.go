package serve

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/platform/gitpush"
	"reasonix/internal/session/control"
)

// gitCalls counts what reached the controller: a refused request must reach nothing.
type gitCalls struct {
	control.SessionAPI
	preview, push, discard int
}

func (g *gitCalls) GitPushPreview(_ context.Context, _ string) (gitpush.Plan, error) {
	g.preview++
	return gitpush.Plan{}, nil
}

func (g *gitCalls) GitPush(_ context.Context, _ control.PushRequest) (gitpush.Result, error) {
	g.push++
	return gitpush.Result{}, nil
}

func (g *gitCalls) DiscardChange(_ context.Context, _ control.DiscardRequest) error {
	g.discard++
	return nil
}

func gitRoute(g *gitCalls, method, path, body string, device bool) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	(&Server{ctrl: g}).registerGitRoutes(mux)
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if device {
		req = req.WithContext(withDeviceReach(req.Context(), "phone-1", 1))
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestAPairedDeviceCannotPreviewPushOrDiscardAndTheHostCan(t *testing.T) {
	routes := []struct {
		name, method, path, body string
		count                    func(*gitCalls) int
	}{
		{"preview a push", http.MethodGet, "/git/push", "", func(g *gitCalls) int { return g.preview }},
		{"push", http.MethodPost, "/git/push", `{"plan":{}}`, func(g *gitCalls) int { return g.push }},
		{"discard", http.MethodPost, "/changes/discard", `{"path":"a.go"}`, func(g *gitCalls) int { return g.discard }},
	}
	for _, rt := range routes {
		device := &gitCalls{}
		rec := gitRoute(device, rt.method, rt.path, rt.body, true)
		var reason Reason
		_ = json.Unmarshal(rec.Body.Bytes(), &reason)
		if rec.Code != http.StatusNotFound || reason.Code != codeDeviceHostOnly || rt.count(device) != 0 {
			t.Errorf("%s from a device: status %d code %q reached the controller %d times, want 404 %s and never", rt.name, rec.Code, reason.Code, rt.count(device), codeDeviceHostOnly)
		}
		host := &gitCalls{}
		rec = gitRoute(host, rt.method, rt.path, rt.body, false)
		if rec.Code == http.StatusNotFound || rt.count(host) != 1 {
			t.Errorf("%s from the host: status %d, reached the controller %d times, want it served once", rt.name, rec.Code, rt.count(host))
		}
	}
}

// A remote pane's requests are forwarded with the host's own token, so the far
// kernel sees the host, not the device: the refusal has to come before the proxy.
func TestAPairedDeviceCannotReachThePushOrDiscardOfARemotePaneAndTheHostIsForwarded(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	rk := fakeRemoteKernel(t)
	h := NewHub(HubOptions{})
	rt := remotePane(t, h, rk.Listener.Addr().String())
	prefix := rt.view().Base
	routes := []struct{ method, path string }{
		{http.MethodGet, "/git/push"},
		{http.MethodPost, "/git/push"},
		{http.MethodPost, "/changes/discard"},
		{http.MethodPost, "/isolated-results/discard-preview"},
		{http.MethodPost, "/isolated-results/discard"},
		{http.MethodPost, "/isolated-results/recover"},
		{http.MethodPost, "/isolated-results/command"},
		{http.MethodPost, "/isolated-results/root"},
		{http.MethodGet, "//git/./push"},
		{http.MethodHead, "/git/push"},
		{http.MethodPost, "/changes/../changes/discard"},
	}
	serve := func(method, path string, device bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, prefix+path, strings.NewReader("{}"))
		if device {
			req = req.WithContext(withDeviceReach(req.Context(), "phone-1", 1))
		}
		rec := httptest.NewRecorder()
		rt.handler.ServeHTTP(rec, req)
		return rec
	}
	drain := func() int {
		n := 0
		for {
			select {
			case <-rk.cookies:
				n++
			default:
				return n
			}
		}
	}
	drain()
	for _, rt := range routes {
		rec := serve(rt.method, rt.path, true)
		var reason Reason
		_ = json.Unmarshal(rec.Body.Bytes(), &reason)
		if rec.Code != http.StatusNotFound || reason.Code != codeDeviceHostOnly {
			t.Errorf("%s %s from a device = %d %q, want 404 %s", rt.method, rt.path, rec.Code, reason.Code, codeDeviceHostOnly)
		}
		if n := drain(); n != 0 {
			t.Fatalf("the remote kernel saw %d requests from a device for %s, want none", n, rt.path)
		}
	}
	for _, rt := range routes {
		rec := serve(rt.method, rt.path, false)
		if rec.Code == http.StatusNotFound && strings.Contains(rec.Body.String(), codeDeviceHostOnly) {
			t.Errorf("%s %s from the host was refused as a device", rt.method, rt.path)
		}
		if n := drain(); n != 1 {
			t.Fatalf("the remote kernel saw %d requests from the host for %s, want one", n, rt.path)
		}
	}
}

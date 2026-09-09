package servepool

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestManager(t *testing.T, roots ...string) *Manager {
	t.Helper()
	m, err := NewManager(Config{
		ReasonixBin:  "definitely-not-a-real-binary", // spawn always fails; exercises failure/degraded paths
		ProjectRoots: roots,
		IdleTimeout:  50 * time.Millisecond,
		SpawnTimeout: 300 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)
	return m
}

func bearer(token string) map[string]string {
	if token == "" {
		return map[string]string{}
	}
	return map[string]string{"Authorization": "Bearer " + token}
}

func TestGatewayAuth(t *testing.T) {
	m := newTestManager(t)
	g := NewGateway(m, "secret", nil)
	ts := httptest.NewServer(g)
	defer ts.Close()

	// Missing / wrong / correct token.
	for name, hdr := range map[string]map[string]string{
		"missing": bearer(""),
		"wrong":   bearer("nope"),
		"correct": bearer("secret"),
	} {
		req, _ := http.NewRequest(http.MethodGet, ts.URL+"/manifest", nil)
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		want := http.StatusUnauthorized
		if name == "correct" {
			want = http.StatusOK
		}
		if resp.StatusCode != want {
			t.Fatalf("%s: status = %d, want %d", name, resp.StatusCode, want)
		}
	}
}

func TestGatewayManifestShape(t *testing.T) {
	root := filepath.Join(t.TempDir(), "proj")
	m := newTestManager(t, root)
	g := NewGateway(m, "s", nil)
	ts := httptest.NewServer(g)
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/manifest", nil)
	req.Header.Set("Authorization", "Bearer s")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out []ProjectState
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].State != "stopped" || out[0].Root != filepath.Clean(root) {
		t.Fatalf("manifest = %+v, want one stopped project", out)
	}
}

func TestGatewayOpenMissingID(t *testing.T) {
	m := newTestManager(t)
	g := NewGateway(m, "s", nil)
	ts := httptest.NewServer(g)
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/projects/open", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer s")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestGatewayProxyMalformed(t *testing.T) {
	m := newTestManager(t)
	g := NewGateway(m, "s", nil)
	ts := httptest.NewServer(g)
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/p/", nil)
	req.Header.Set("Authorization", "Bearer s")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestManagerUnknownProject(t *testing.T) {
	m := newTestManager(t)
	if err := m.Open("nope"); err == nil {
		t.Fatal("Open unknown project succeeded; want error")
	}
}

func TestManagerRefreshProjects(t *testing.T) {
	a := filepath.Join(t.TempDir(), "a")
	b := filepath.Join(t.TempDir(), "b")
	m := newTestManager(t, a)
	if len(m.Projects()) != 1 {
		t.Fatalf("projects = %d, want 1", len(m.Projects()))
	}
	m.RefreshProjects([]string{a, b})
	if len(m.Projects()) != 2 {
		t.Fatalf("after refresh = %d, want 2", len(m.Projects()))
	}
	m.RefreshProjects([]string{a})
	if len(m.Projects()) != 1 {
		t.Fatalf("after removal = %d, want 1", len(m.Projects()))
	}
}

func TestManagerSpawnFailureAndDegraded(t *testing.T) {
	root := filepath.Join(t.TempDir(), "proj")
	m := newTestManager(t, root)

	// First open: spawn fails (fake binary) -> stopped with error.
	if err := m.Open("proj"); err == nil {
		t.Fatal("Open with failing binary succeeded; want error")
	}
	ps := m.Projects()
	if ps[0].State != "stopped" || ps[0].Err == "" {
		t.Fatalf("after first failure: %+v, want stopped+err", ps[0])
	}

	// Three consecutive failures -> degraded with a cooldown that refuses Open.
	for i := 0; i < 2; i++ {
		_ = m.Open("proj")
	}
	ps = m.Projects()
	if ps[0].State != "degraded" {
		t.Fatalf("state = %s, want degraded", ps[0].State)
	}
	if err := m.Open("proj"); err == nil {
		t.Fatal("Open during degraded cooldown succeeded; want refusal")
	}
}

func TestManagerIdleReclaim(t *testing.T) {
	root := filepath.Join(t.TempDir(), "proj")
	m := newTestManager(t, root)
	// Force the project into a "running-like" state is not possible without a
	// real serve; verify the sweep path is safe on stopped projects and that
	// a project removed by Refresh is gone after the sweep.
	m.RefreshProjects(nil)
	time.Sleep(120 * time.Millisecond)
	if len(m.Projects()) != 0 {
		t.Fatalf("projects = %d, want 0 after removal", len(m.Projects()))
	}
}


// fakeHook records HandoffSession calls and returns a configurable error.
type fakeHook struct {
	calls int
	err   error
}

func (f *fakeHook) HandoffSession(projectID, sessionName, targetWriterID string) error {
	f.calls++
	if f.err != nil {
		return f.err
	}
	return nil
}

func TestTakeoverRetryTransport(t *testing.T) {
	backendCalls := 0
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		backendCalls++
		if backendCalls == 1 {
			http.Error(w, `{"error":"session is held by another runtime"}`, http.StatusConflict)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer backend.Close()
	u, err := url.Parse(backend.URL)
	if err != nil {
		t.Fatal(err)
	}
	hook := &fakeHook{}
	tr := &takeoverRetryTransport{base: http.DefaultTransport, hook: hook, projectID: "proj"}

	// takeover-session 409 -> hook invoked -> retried once -> 204.
	req, _ := http.NewRequest(http.MethodPost, u.String()+"/takeover-session", strings.NewReader(`{"name":"s1","from":"w1"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 after handoff retry", resp.StatusCode)
	}
	if hook.calls != 1 {
		t.Fatalf("hook calls = %d, want 1", hook.calls)
	}
	if backendCalls != 2 {
		t.Fatalf("backend calls = %d, want 2 (409 + retry)", backendCalls)
	}

	// Non-takeover paths never invoke the hook.
	backendCalls = 0
	req2, _ := http.NewRequest(http.MethodGet, u.String()+"/sessions", nil)
	resp2, err := tr.RoundTrip(req2)
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if hook.calls != 1 {
		t.Fatalf("hook calls after plain request = %d, want still 1", hook.calls)
	}

	// Hook failure leaves the 409 untouched (no retry).
	backendCalls = 0
	hookErr := &fakeHook{err: errors.New("not held by this host")}
	trErr := &takeoverRetryTransport{base: http.DefaultTransport, hook: hookErr, projectID: "proj"}
	req3, _ := http.NewRequest(http.MethodPost, u.String()+"/takeover-session", strings.NewReader(`{"name":"s1","from":"w1"}`))
	resp3, err := trErr.RoundTrip(req3)
	if err != nil {
		t.Fatal(err)
	}
	resp3.Body.Close()
	if resp3.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409 when hook fails", resp3.StatusCode)
	}
	if backendCalls != 1 {
		t.Fatalf("backend calls = %d, want 1 (no retry on hook failure)", backendCalls)
	}
}

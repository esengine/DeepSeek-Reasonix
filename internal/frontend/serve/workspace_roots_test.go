package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/session/control"
)

func TestWorkspaceRootsRoutesAddRebuildAndRemove(t *testing.T) {
	workspace, extra := testenv.TempDir(t), tempAside(t)()
	s := newProviderEditServer(t, workspace)
	s.AllowProviderEdit()
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()

	before := s.Controller()
	resp := postProvider(t, srv.URL, "/workspace/roots", `{"path":`+quote(extra)+`}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := readAllString(resp)
		t.Fatalf("add = %d: %s", resp.StatusCode, body)
	}
	if s.Controller() == before {
		t.Fatal("the grant was written without rebuilding the runtime that reads it")
	}
	got := readJSON[workspaceRootsView](t, srv.URL, "/workspace/roots")
	if got.Primary != workspace || len(got.Extra) != 1 {
		t.Fatalf("roots = %+v", got)
	}

	resp = postProvider(t, srv.URL, "/workspace/roots/remove", `{"path":`+quote(extra)+`}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("remove = %d", resp.StatusCode)
	}
	if got = readJSON[workspaceRootsView](t, srv.URL, "/workspace/roots"); len(got.Extra) != 0 {
		t.Fatalf("roots after removal = %+v", got)
	}
}

func TestWorkspaceRootsRefusalsCarryTheirIdentity(t *testing.T) {
	workspace := testenv.TempDir(t)
	s := newProviderEditServer(t, workspace)
	s.AllowProviderEdit()
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()
	for body, want := range map[string]string{
		`{"path":"relative"}`:               string(control.RootNotDirectory),
		`{"path":` + quote(workspace) + `}`: string(control.RootCoveredAlready),
		`{"path":""}`:                       "workspace_roots.path_required",
	} {
		resp := postProvider(t, srv.URL, "/workspace/roots", body)
		var reason Reason
		_ = json.NewDecoder(resp.Body).Decode(&reason)
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest || reason.Code != want {
			t.Errorf("%s -> %d %q, want 400 %q", body, resp.StatusCode, reason.Code, want)
		}
	}
	if !slices.Equal(readJSON[workspaceRootsView](t, srv.URL, "/workspace/roots").Extra, []string{}) {
		t.Fatal("a refused folder was recorded")
	}
}

func quote(s string) string { b, _ := json.Marshal(s); return string(b) }

func TestWorkspaceRootsEditingIsGated(t *testing.T) {
	s := newProviderEditServer(t, testenv.TempDir(t))
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()
	resp := postProvider(t, srv.URL, "/workspace/roots", `{"path":`+quote(tempAside(t)())+`}`)
	defer resp.Body.Close()
	var reason Reason
	_ = json.NewDecoder(resp.Body).Decode(&reason)
	if resp.StatusCode != http.StatusForbidden || reason.Code != "workspace_roots.editing_disabled" {
		t.Fatalf("ungranted add = %d %q", resp.StatusCode, reason.Code)
	}
}

func TestAPairedDeviceCannotGrantARootEvenWhenTheWindowCan(t *testing.T) {
	s := newProviderEditServer(t, testenv.TempDir(t))
	s.AllowProviderEdit()
	inner := operatorHandler(s)
	device := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		inner.ServeHTTP(w, r.WithContext(withDeviceReach(r.Context(), "phone", 1)))
	})
	srv := httptest.NewServer(device)
	defer srv.Close()
	resp := postProvider(t, srv.URL, "/workspace/roots", `{"path":`+quote(tempAside(t)())+`}`)
	defer resp.Body.Close()
	var reason Reason
	_ = json.NewDecoder(resp.Body).Decode(&reason)
	if resp.StatusCode != http.StatusForbidden || reason.Code != "workspace_roots.editing_disabled" {
		t.Fatalf("a paired device's grant = %d %q", resp.StatusCode, reason.Code)
	}
}

func TestEveryWorkspaceRootRefusalIsTranslated(t *testing.T) {
	said := translatedCodes(t)
	for _, code := range []config.WorkspaceRootRefusal{config.RootNotDirectory, config.RootCoveredAlready, config.RootTooBroad, config.RootProtected, config.RootNotOwned, config.RootOverlapsWrite} {
		if !said[string(code)] {
			t.Errorf("refusal %s has no text in the frontend catalogue", code)
		}
	}
}

func TestSandboxAllowWriteRefusesWhatARootRefuses(t *testing.T) {
	s := newProviderEditServer(t, testenv.TempDir(t))
	s.AllowProviderEdit()
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()
	ssh := filepath.Join(os.Getenv("HOME"), ".ssh")
	if err := os.MkdirAll(ssh, 0o700); err != nil {
		t.Fatal(err)
	}
	resp := postProvider(t, srv.URL, "/sandbox", `{"bash":"","network":true,"allowWrite":[`+quote(ssh)+`]}`)
	defer resp.Body.Close()
	var reason Reason
	_ = json.NewDecoder(resp.Body).Decode(&reason)
	if resp.StatusCode != http.StatusBadRequest || reason.Code != string(config.RootProtected) {
		t.Fatalf("allow_write of ~/.ssh = %d %q", resp.StatusCode, reason.Code)
	}
}

func TestWithdrawingARootRebuildsEveryPaneOnTheWorkspace(t *testing.T) {
	workspace := testenv.TempDir(t)
	extra := tempAside(t)()
	first := newProviderEditServer(t, workspace)
	bc := NewBroadcaster()
	ctrl := control.New(control.Options{Sink: bc, Label: "model-a", ModelRef: "existing/model-a", SessionDir: testenv.TempDir(t), WorkspaceRoot: workspace})
	t.Cleanup(ctrl.Close)
	second := New(ctrl, bc, config.ServeConfig{})
	h := NewHub(HubOptions{Grant: func(s *Server) { s.AllowProviderEdit() }})
	a, err := h.Adopt(first, first.bc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.Adopt(second, bc); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(operatorHandler(h))
	defer srv.Close()
	before := second.Controller()
	resp, err := http.Post(srv.URL+runtimePrefix+a.ID+"/workspace/roots", "application/json", strings.NewReader(`{"path":`+quote(extra)+`}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("add = %d", resp.StatusCode)
	}
	if second.Controller() == before {
		t.Fatal("another pane on the same workspace kept its old write roots")
	}
}

func TestTwoPanesEditingRootsAtOnceDoNotWaitOnEachOther(t *testing.T) {
	workspace := testenv.TempDir(t)
	extraFolder := tempAside(t)
	first := newProviderEditServer(t, workspace)
	bc := NewBroadcaster()
	ctrl := control.New(control.Options{Sink: bc, Label: "model-a", ModelRef: "existing/model-a", SessionDir: testenv.TempDir(t), WorkspaceRoot: workspace})
	t.Cleanup(ctrl.Close)
	second := New(ctrl, bc, config.ServeConfig{})
	h := NewHub(HubOptions{Grant: func(s *Server) { s.AllowProviderEdit() }})
	a, err := h.Adopt(first, first.bc)
	if err != nil {
		t.Fatal(err)
	}
	b, err := h.Adopt(second, bc)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(operatorHandler(h))
	defer srv.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		var wg sync.WaitGroup
		for i := range 6 {
			for _, id := range []string{a.ID, b.ID} {
				wg.Go(func() {
					path := "/workspace/roots"
					if i%2 == 1 {
						path = "/workspace/roots/remove"
					}
					resp, err := http.Post(srv.URL+runtimePrefix+id+path, "application/json", strings.NewReader(`{"path":`+quote(extraFolder())+`}`))
					if err == nil {
						resp.Body.Close()
					}
				})
			}
		}
		wg.Wait()
	}()
	select {
	case <-done:
	case <-time.After(60 * time.Second):
		t.Fatal("two panes on one workspace editing roots at once deadlocked")
	}
}

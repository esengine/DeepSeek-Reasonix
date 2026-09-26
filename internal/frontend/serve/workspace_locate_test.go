package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/session/control"
)

func locateServer(t *testing.T, root string, granted bool) *Server {
	t.Helper()
	ctrl := control.New(control.Options{Runner: fakeRunner{}, WorkspaceRoot: root})
	t.Cleanup(ctrl.Close)
	s := New(ctrl, NewBroadcaster(), config.ServeConfig{})
	if granted {
		s.AllowLocalDesktop()
	}
	return s
}

func locate(s *Server, r *http.Request) (int, workspaceLocation, Reason) {
	rec := httptest.NewRecorder()
	s.workspaceLocate(rec, r)
	var at workspaceLocation
	var why Reason
	if rec.Code == http.StatusOK {
		_ = json.Unmarshal(rec.Body.Bytes(), &at)
	} else {
		_ = json.Unmarshal(rec.Body.Bytes(), &why)
	}
	return rec.Code, at, why
}

func locateRequest(path string) *http.Request {
	return httptest.NewRequest(http.MethodGet, "/workspace/locate?path="+url.QueryEscape(path), nil)
}

func TestLocateNamesOnlyEntriesInsideTheWorkspace(t *testing.T) {
	root := testenv.TempDir(t)
	if err := os.MkdirAll(filepath.Join(root, "out", "t620"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "out", "report.html"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	outside := testenv.TempDir(t)
	s := locateServer(t, root, true)
	abs, _ := filepath.Abs(root)

	for _, tc := range []struct {
		path string
		want string
		dir  bool
	}{
		{"", abs, true},
		{".", abs, true},
		{"./", abs, true},
		{"out/t620", filepath.Join(abs, "out", "t620"), true},
		{"out/report.html", filepath.Join(abs, "out", "report.html"), false},
		{"out/./t620/../report.html", filepath.Join(abs, "out", "report.html"), false},
	} {
		code, at, why := locate(s, locateRequest(tc.path))
		if code != http.StatusOK || at.Path != tc.want || at.Dir != tc.dir {
			t.Errorf("locate %q = %d %+v %+v, want %q dir=%v", tc.path, code, at, why, tc.want, tc.dir)
		}
	}

	for _, escape := range []string{"..", "../" + filepath.Base(outside), "out/../../x", outside, filepath.ToSlash(outside)} {
		code, at, why := locate(s, locateRequest(escape))
		if code != http.StatusBadRequest || why.Code != "workspace.path_outside_tree" {
			t.Errorf("locate %q = %d %+v %+v, want a path_outside_tree refusal", escape, code, at, why)
		}
	}

	if code, _, why := locate(s, locateRequest("out/gone")); code != http.StatusNotFound || why.Code != "workspace.file_missing" {
		t.Errorf("a missing entry = %d %+v, want workspace.file_missing", code, why)
	}
}

// A link is spelled inside the tree and lands outside it; the answer follows
// where it lands, which is the only place a file manager would open.
func TestLocateRefusesALinkThatLeavesTheWorkspace(t *testing.T) {
	root := testenv.TempDir(t)
	outside := testenv.TempDir(t)
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Skipf("symlinks unavailable here: %v", err)
	}
	code, at, why := locate(locateServer(t, root, true), locateRequest("escape"))
	if code != http.StatusBadRequest || why.Code != "workspace.path_outside_tree" {
		t.Fatalf("locate through a link = %d %+v %+v, want a path_outside_tree refusal", code, at, why)
	}
}

// The answer is a path on the kernel's disk, useful only to a shell on the
// same machine: a networked serve and a paired device get the refusal.
func TestLocateBelongsToTheLocalWindowOnly(t *testing.T) {
	root := testenv.TempDir(t)
	if code, _, why := locate(locateServer(t, root, false), locateRequest("")); code != http.StatusForbidden || why.Code != codeLocateNoWindow {
		t.Errorf("without the grant = %d %+v, want %s", code, why, codeLocateNoWindow)
	}
	device := locateRequest("")
	device = device.WithContext(withDeviceReach(device.Context(), "phone", 1))
	if code, _, why := locate(locateServer(t, root, true), device); code != http.StatusForbidden || why.Code != codeLocateNoWindow {
		t.Errorf("from a paired device = %d %+v, want %s", code, why, codeLocateNoWindow)
	}
}

// A remote pane's kernel answers with paths on its own disk, or with whatever
// it likes; the hub knows the pane is remote and answers before proxying.
func TestHubRefusesToLocateInARemoteWorkspace(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	rk := fakeRemoteKernel(t)
	h := NewHub(HubOptions{})
	rt := remotePane(t, h, rk.Listener.Addr().String())
	front := httptest.NewServer(h.Handler())
	defer front.Close()

	for _, path := range []string{"/workspace/locate", "/workspace/locate?path=out", "//workspace/./locate", "/workspace/locate/"} {
		resp, err := http.Get(front.URL + rt.view().Base + path)
		if err != nil {
			t.Fatal(err)
		}
		var why Reason
		_ = json.NewDecoder(resp.Body).Decode(&why)
		resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden || why.Code != codeLocateNoWindow {
			t.Errorf("GET %s on a remote pane = %d %+v, want %s", path, resp.StatusCode, why, codeLocateNoWindow)
		}
	}
	select {
	case <-rk.cookies:
		t.Fatal("the locate request reached the remote kernel")
	default:
	}
}

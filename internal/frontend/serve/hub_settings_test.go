package serve

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/platform/remotecloud"
)

func TestMachineSettingsFollowLiveLocalRuntime(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	h := NewHub(HubOptions{})
	far := fakeRemoteKernel(t)
	remotePane(t, h, far.Listener.Addr().String())
	local := hubRuntime(t, h, testenv.TempDir(t))
	handler := operatorHandler(h)
	request := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		req := httptest.NewRequest(method, "http://localhost/host/settings"+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		handler.ServeHTTP(w, req)
		return w
	}
	read := func() {
		t.Helper()
		w := request(http.MethodGet, "/appearance", "")
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"zoomRange"`) {
			t.Fatalf("machine appearance = %d %s", w.Code, w.Body.String())
		}
	}
	read()
	w := request(http.MethodPost, "/appearance", `{"language":"en"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("save machine appearance = %d %s", w.Code, w.Body.String())
	}
	if err := h.Close(local.ID); err != nil {
		t.Fatal(err)
	}
	w = request(http.MethodGet, "/appearance", "")
	if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), `"settings.local_runtime_required"`) {
		t.Fatalf("remote-only machine settings = %d %s", w.Code, w.Body.String())
	}
	hubRuntime(t, h, testenv.TempDir(t))
	read()
	w = request(http.MethodPost, "/submit", `{"text":"must not run"}`)
	if w.Code != http.StatusNotFound {
		t.Fatalf("session action = %d", w.Code)
	}
	select {
	case cookie := <-far.cookies:
		t.Fatalf("machine request reached remote: %s", cookie)
	default:
	}
}

func TestMachineSettingsPreserveRequestAuthority(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	h := NewHub(HubOptions{})
	hubRuntime(t, h, testenv.TempDir(t))
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		response, err := h.CloudDesktop(t.Context(), remotecloud.DesktopRequest{
			Method: method, Path: "/host/settings/appearance", Body: []byte(`{"language":"en"}`),
		}, "phone")
		if err != nil {
			t.Fatal(err)
		}
		if response.Status != http.StatusOK {
			t.Fatalf("paired %s = %d: %s", method, response.Status, response.Body)
		}
	}
	req := httptest.NewRequest(http.MethodPost, "http://localhost/host/settings/appearance", strings.NewReader(`{"language":"en"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("unauthenticated mutation = %d", w.Code)
	}
	req.Header.Set("Authorization", "Bearer "+h.AuthToken())
	req.Header.Set("Origin", "https://untrusted.example")
	w = httptest.NewRecorder()
	h.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("bearer-authorized mutation = %d", w.Code)
	}
}

func TestMachineSettingsAssetsStayLocalWithRemoteFirst(t *testing.T) {
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	h := NewHub(HubOptions{})
	far := fakeRemoteKernel(t)
	remotePane(t, h, far.Listener.Addr().String())
	hubRuntime(t, h, testenv.TempDir(t))
	name, err := storeWallpaper("image/png", strings.NewReader("local wallpaper fixture"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte("[desktop.appearance.wallpaper]\nfile = \""+name+"\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/themes/official-noir-gold/preview", "/themes/official-noir-gold/background", "/appearance/wallpaper/" + name} {
		w := httptest.NewRecorder()
		operatorHandler(h).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "http://localhost"+path, nil))
		if w.Code != http.StatusOK || w.Body.Len() == 0 {
			t.Fatalf("asset %s = %d %s", path, w.Code, w.Body.String())
		}
	}
	select {
	case cookie := <-far.cookies:
		t.Fatalf("machine asset reached remote: %s", cookie)
	default:
	}
}

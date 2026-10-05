package serve

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/platform/remotecloud"
)

func TestCloudDesktopServesTheDeviceSafeStudioSurface(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	hub := NewHub(HubOptions{})
	runtime := hubRuntime(t, hub, testenv.TempDir(t))

	response, err := hub.CloudDesktop(t.Context(), remotecloud.DesktopRequest{
		Method: http.MethodGet, Path: "/runtimes",
	}, "phone")
	if err != nil {
		t.Fatal(err)
	}
	if response.Status != http.StatusOK || response.ContentType != "application/json" {
		t.Fatalf("response = %+v", response)
	}
	var runtimes []RuntimeView
	if err := json.Unmarshal(response.Body, &runtimes); err != nil {
		t.Fatal(err)
	}
	if len(runtimes) != 1 || runtimes[0].ID != runtime.ID {
		t.Fatalf("runtimes = %+v", runtimes)
	}

	hostOnly, err := hub.CloudDesktop(t.Context(), remotecloud.DesktopRequest{
		Method: http.MethodPost, Path: "/host/pick-folder", Body: []byte(`{}`),
	}, "phone")
	if err != nil {
		t.Fatal(err)
	}
	if hostOnly.Status != http.StatusNotFound {
		t.Fatalf("host-only status = %d, want 404", hostOnly.Status)
	}
}

func TestCloudDesktopRejectsLiveStreamsAndOversizedWrites(t *testing.T) {
	hub := NewHub(HubOptions{})
	for _, request := range []remotecloud.DesktopRequest{
		{Method: http.MethodGet, Path: "/events"},
		{Method: http.MethodGet, Path: "/rt/runtime/events"},
		{Method: http.MethodPost, Path: "/submit", Body: make([]byte, cloudDesktopRequestLimit+1)},
		{Method: http.MethodConnect, Path: "/runtimes"},
		{Method: http.MethodGet, Path: "https://example.com/runtimes"},
	} {
		if _, err := hub.CloudDesktop(t.Context(), request, "phone"); !errors.Is(err, ErrCloudDesktopRequest) {
			t.Errorf("CloudDesktop(%+v) = %v, want refused", request, err)
		}
	}
}

func TestCloudDesktopBodylessPostReachesTheBackendAsJSON(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	hub := NewHub(HubOptions{})
	runtime := hubRuntime(t, hub, testenv.TempDir(t))

	response, err := hub.CloudDesktop(t.Context(), remotecloud.DesktopRequest{
		Method: http.MethodPost, Path: "/runtimes/" + runtime.ID + "/close",
	}, "phone")
	if err != nil {
		t.Fatal(err)
	}
	if response.Status != http.StatusNoContent {
		t.Fatalf("bodyless close status = %d, want 204; body %s", response.Status, response.Body)
	}
}

func TestCSRFGuardStillRefusesPostWithoutJSONType(t *testing.T) {
	reached := false
	guarded := csrfGuard(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true }))
	for _, contentType := range []string{"", "text/plain", "application/x-www-form-urlencoded"} {
		req := httptest.NewRequest(http.MethodPost, "/runtimes/x/close", nil)
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		rec := httptest.NewRecorder()
		guarded.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnsupportedMediaType || reached {
			t.Fatalf("content type %q: status = %d, reached = %v", contentType, rec.Code, reached)
		}
	}
}

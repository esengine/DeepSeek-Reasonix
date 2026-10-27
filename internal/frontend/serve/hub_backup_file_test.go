package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
)

func TestMachineSettingsFileBackupRoutes(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	h := NewHub(HubOptions{})
	defer h.Shutdown()
	rt := hubRuntime(t, h, testenv.TempDir(t))
	handler := operatorHandler(h)
	for _, base := range []string{"/host/settings", "/host/settings/runtime/" + rt.ID} {
		t.Run(base, func(t *testing.T) {
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, base+"/backups/file", nil))
			var catalog struct {
				Categories    []json.RawMessage `json:"categories"`
				MinPassphrase int               `json:"minPassphrase"`
			}
			if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &catalog) != nil || len(catalog.Categories) == 0 || catalog.MinPassphrase == 0 {
				t.Fatalf("catalog = %d %s", w.Code, w.Body.String())
			}
			for _, tc := range []struct {
				path, body string
				status     int
			}{
				{"/backups/file/export", `{`, http.StatusBadRequest},
				{"/backups/file/preview", `{"file":"!!","passphrase":"test"}`, http.StatusUnprocessableEntity},
				{"/backups/file/apply", `{"planId":"missing","items":["x"]}`, http.StatusGone},
			} {
				r := httptest.NewRequest(http.MethodPost, base+tc.path, strings.NewReader(tc.body))
				r.Header.Set("Content-Type", "application/json")
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, r)
				if w.Code != tc.status {
					t.Fatalf("%s = %d %s, want %d", tc.path, w.Code, w.Body.String(), tc.status)
				}
				unauthorized := httptest.NewRequest(http.MethodPost, "http://localhost"+base+tc.path, strings.NewReader(tc.body))
				unauthorized.Header.Set("Content-Type", "application/json")
				denied := httptest.NewRecorder()
				h.Handler().ServeHTTP(denied, unauthorized)
				if denied.Code != http.StatusForbidden {
					t.Fatalf("unauthorized %s = %d %s", base+tc.path, denied.Code, denied.Body.String())
				}
			}
		})
	}
}

func TestMachineSettingsFileBackupRefusesPairedDevices(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	h := NewHub(HubOptions{})
	defer h.Shutdown()
	rt := hubRuntime(t, h, testenv.TempDir(t))
	for _, base := range []string{"/host/settings", "/host/settings/runtime/" + rt.ID} {
		for _, tc := range []struct{ method, path string }{
			{http.MethodGet, "/backups/file"},
			{http.MethodPost, "/backups/file/export"},
			{http.MethodPost, "/backups/file/preview"},
			{http.MethodPost, "/backups/file/apply"},
		} {
			r := httptest.NewRequest(tc.method, base+tc.path, strings.NewReader(`{}`))
			r.Header.Set("Content-Type", "application/json")
			r = r.WithContext(withDeviceReach(r.Context(), "phone", 1))
			w := httptest.NewRecorder()
			operatorHandler(h).ServeHTTP(w, r)
			var refusal struct{ Code string }
			if w.Code != http.StatusNotFound || json.Unmarshal(w.Body.Bytes(), &refusal) != nil || refusal.Code != codeDeviceHostOnly {
				t.Fatalf("device %s = %d %s", base+tc.path, w.Code, w.Body.String())
			}
		}
	}
}

func TestMachineSettingsFileBackupRequiresLocalOwner(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	h := NewHub(HubOptions{})
	defer h.Shutdown()
	remote := remotePane(t, h, fakeRemoteKernel(t).Listener.Addr().String())
	request := func(path string) {
		t.Helper()
		w := httptest.NewRecorder()
		operatorHandler(h).ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		var refusal struct{ Code string }
		if w.Code != http.StatusServiceUnavailable || json.Unmarshal(w.Body.Bytes(), &refusal) != nil || refusal.Code != "settings.local_runtime_required" {
			t.Fatalf("%s = %d %s", path, w.Code, w.Body.String())
		}
	}
	request("/host/settings/backups/file")
	hubRuntime(t, h, testenv.TempDir(t))
	request("/host/settings/runtime/" + remote.ID + "/backups/file")
	request("/host/settings/runtime/missing/backups/file")
}

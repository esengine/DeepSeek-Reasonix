package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/session/control"
)

func TestADeviceOpensOnlyFoldersTheWindowAlreadyListsAsRuntimes(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	hub := NewHub(HubOptions{Serve: config.ServeConfig{AuthMode: "none"}})
	defer hub.Shutdown()
	bc := NewBroadcaster()
	if _, err := hub.Adopt(New(control.New(control.Options{Sink: bc}), bc, config.ServeConfig{}), bc); err != nil {
		t.Fatal(err)
	}
	before := len(hub.List())
	elsewhere := testenv.TempDir(t)

	reg := NewDeviceRegistry()
	code, _ := reg.Offer()
	cred, _, err := reg.Redeem(code, "phone")
	if err != nil {
		t.Fatal(err)
	}
	gate := NewDeviceGate(hub.Handler(), DeviceGateOptions{Registry: reg, Origin: "http://192.168.1.5:9000"})
	req := httptest.NewRequest(http.MethodPost, "http://192.168.1.5:9000/runtimes", strings.NewReader(`{"root":`+quoteJSON(elsewhere)+`}`))
	req.Header.Set("Origin", "http://192.168.1.5:9000")
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: DeviceCookie, Value: cred})
	rec := httptest.NewRecorder()
	gate.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), codeDeviceHostOnly) {
		t.Fatalf("a device opening an unlisted folder: %d %s, want 404 %s", rec.Code, rec.Body.String(), codeDeviceHostOnly)
	}
	if after := len(hub.List()); after != before {
		t.Fatalf("a runtime was opened for the device: %d -> %d", before, after)
	}
}

func TestDeviceMayOpenRootNamesOnlyKnownFolders(t *testing.T) {
	known := []string{"/work/a", "/work/b"}
	for root, want := range map[string]bool{"": true, "/work/a": true, "/work/a/": true, "/work/b/../b": true, "/work": false, "/etc": false, "/work/a/sub": false} {
		if got := deviceMayOpenRoot(root, known); got != want {
			t.Errorf("deviceMayOpenRoot(%q) = %v, want %v", root, got, want)
		}
	}
}

func quoteJSON(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

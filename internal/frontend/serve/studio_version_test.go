package serve

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/platform/update"
)

func studioServer(t *testing.T, in *update.Install) *httptest.Server {
	t.Helper()
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	h := NewHub(HubOptions{Install: in})
	srv := httptest.NewServer(operatorHandler(h))
	t.Cleanup(srv.Close)
	return srv
}

// A kernel nobody declared an install for is not a Studio: it has no build of
// its own to report or change. It says so with a code, because "not a Studio"
// and "a catalog that came back empty" are different answers and a panel that
// folds them together offers to update a server with no application around it.
func TestVersionRoutesRefuseByNameWithoutAnInstall(t *testing.T) {
	srv := studioServer(t, nil)

	resp, err := http.Get(srv.URL + "/studio/versions")
	if err != nil {
		t.Fatal(err)
	}
	if code := refusalCode(t, resp); code != codeNoInstall {
		t.Fatalf("GET /studio/versions refused with %q, want %q", code, codeNoInstall)
	}

	resp, err = http.Post(srv.URL+"/studio/pin", "application/json", bytes.NewReader([]byte(`{"version":"2.9.0"}`)))
	if err != nil {
		t.Fatal(err)
	}
	if code := refusalCode(t, resp); code != codeNoInstall {
		t.Fatalf("POST /studio/pin refused with %q, want %q", code, codeNoInstall)
	}
}

func refusalCode(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	var body struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	return body.Code
}

// Pinning is the half of a rollback that outlives the install, so what the
// route must actually do is write it where the next launch reads it.
func TestPinRouteWritesTheHoldAndReleasesIt(t *testing.T) {
	srv := studioServer(t, &update.Install{Version: "2.10.0"})

	pin := func(body string) int {
		t.Helper()
		resp, err := http.Post(srv.URL+"/studio/pin", "application/json", bytes.NewReader([]byte(body)))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}

	if code := pin(`{"version":"2.9.0"}`); code != http.StatusNoContent {
		t.Fatalf("pin = %d, want 204", code)
	}
	if got := config.LoadForEdit(config.UserConfigPath()).DesktopPinnedVersion(); got != "2.9.0" {
		t.Fatalf("pinned version on disk = %q, want 2.9.0", got)
	}
	// An empty version is a value, not a missing field: it is the only way back
	// to following the catalog.
	if code := pin(`{"version":""}`); code != http.StatusNoContent {
		t.Fatalf("release = %d, want 204", code)
	}
	if got := config.LoadForEdit(config.UserConfigPath()).DesktopPinnedVersion(); got != "" {
		t.Fatalf("pinned version on disk = %q, want the hold released", got)
	}
}

func TestPinRouteRefusesABodyItCannotRead(t *testing.T) {
	srv := studioServer(t, &update.Install{Version: "2.10.0"})
	resp, err := http.Post(srv.URL+"/studio/pin", "application/json", bytes.NewReader([]byte(`not json`)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	var body struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	// The identity, not the sentence: a frontend tells refusals apart by code.
	if body.Code != codePinRejected {
		t.Fatalf("code = %q, want %q", body.Code, codePinRejected)
	}
}

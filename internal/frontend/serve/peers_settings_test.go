package serve

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/session/control"
)

func standbyServer(t *testing.T, allowEdit bool) *httptest.Server {
	t.Helper()
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	if err := os.WriteFile(config.UserConfigPath(), []byte("[peers]\nenabled = false\ndispatch = false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	dir := testenv.TempDir(t)
	path := filepath.Join(dir, "sessions", "me.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	bc := NewBroadcaster()
	ctrl := control.New(control.Options{
		Sink: bc, Label: "me", ModelRef: config.UnconfiguredRef, SessionDir: filepath.Dir(path), SessionPath: path,
		Peers: control.PeersOptions{Dir: filepath.Join(dir, "peers"), Standby: true},
	})
	t.Cleanup(ctrl.Close)
	server := New(ctrl, bc, config.ServeConfig{})
	if allowEdit {
		server.AllowProviderEdit()
	}
	srv := httptest.NewServer(operatorHandler(server))
	t.Cleanup(srv.Close)
	return srv
}

func getSettings(t *testing.T, base string) control.PeersSettings {
	t.Helper()
	res, err := http.Get(base + "/peers/settings")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	var s control.PeersSettings
	if err := json.Unmarshal(raw, &s); err != nil || res.StatusCode != http.StatusOK {
		t.Fatalf("settings = %d %s (%v)", res.StatusCode, raw, err)
	}
	return s
}

func TestPeersSettingsRoundTripAndTheirRefusals(t *testing.T) {
	srv := standbyServer(t, true)
	if s := getSettings(t, srv.URL); s.Enabled || !s.Available || s.Applies != "next_turn" || s.Path == "" {
		t.Fatalf("fresh settings: %+v", s)
	}
	if status, raw := postSide(t, srv.URL+"/peers/settings", map[string]any{}); status != http.StatusBadRequest || !strings.Contains(string(raw), "request.bad_body") {
		t.Fatalf("empty body = %d %s", status, raw)
	}
	root := testenv.TempDir(t)
	if status, raw := postSide(t, srv.URL+"/peers/settings", map[string]any{"release_projects": []string{"relative"}}); status != http.StatusBadRequest || !strings.Contains(string(raw), codePeerBadProject) {
		t.Fatalf("relative project = %d %s", status, raw)
	}
	if status, raw := postSide(t, srv.URL+"/peers/settings", map[string]any{"enabled": true, "dispatch": true, "release_projects": []string{root}}); status != http.StatusOK {
		t.Fatalf("save = %d %s", status, raw)
	}
	s := getSettings(t, srv.URL)
	if !s.Enabled || !s.Dispatch || s.Pending || len(s.ReleaseProjects) != 1 || s.ReleaseProjects[0].Path != root {
		t.Fatalf("after save: %+v", s)
	}
	if status, _ := postSide(t, srv.URL+"/peers/settings", map[string]any{"release_projects": []string{}}); status != http.StatusOK {
		t.Fatal("revoking the last project failed")
	}
	if s := getSettings(t, srv.URL); len(s.ReleaseProjects) != 0 || !s.Enabled {
		t.Fatalf("after revoke: %+v", s)
	}
	if status, raw := postSide(t, srv.URL+"/peers/held/m-nope", map[string]any{"approve": false, "remember": "project"}); status != http.StatusBadRequest || !strings.Contains(string(raw), codePeerRemember) {
		t.Fatalf("remembered denial = %d %s", status, raw)
	}
	if status, _ := postSide(t, srv.URL+"/peers/held/m-nope", map[string]any{"approve": true, "remember": "always"}); status != http.StatusBadRequest {
		t.Fatalf("unknown remember = %d", status)
	}
}

func TestANetworkedClientCannotTurnMessagesOn(t *testing.T) {
	srv := standbyServer(t, false)
	if status, raw := postSide(t, srv.URL+"/peers/settings", map[string]any{"enabled": true}); status != http.StatusForbidden {
		t.Fatalf("save without the grant = %d %s", status, raw)
	}
	if config.UserPeers().Enabled {
		t.Fatal("the refused save still wrote the user file")
	}
}

func TestANetworkedClientCannotAllowAProjectThrough(t *testing.T) {
	srv := standbyServer(t, false)
	body := map[string]any{"release_projects": []string{t.TempDir()}}
	if status, raw := postSide(t, srv.URL+"/peers/settings", body); status != http.StatusForbidden {
		t.Fatalf("allowing a project without the grant = %d %s", status, raw)
	}
	if got := config.UserPeers().ReleaseProjects; len(got) != 0 {
		t.Fatalf("the refused save still wrote the user file: %v", got)
	}
}

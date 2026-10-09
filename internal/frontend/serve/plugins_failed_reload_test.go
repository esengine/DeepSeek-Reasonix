package serve

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/ext/pluginpkg"
	"reasonix/internal/session/control"
)

type installBusyController struct{ pluginCtl }

func (c *installBusyController) RuntimeStatus() control.RuntimeStatus {
	return control.RuntimeStatus{BackgroundJobs: 1}
}

func TestFailedPluginInstallKeepsActionFailureAlongsideReloadRefusal(t *testing.T) {
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	ctl := &installBusyController{pluginCtl: pluginCtl{root: testenv.TempDir(t)}}
	srv := httptest.NewServer(operatorHandler(New(ctl, NewBroadcaster(), config.ServeConfig{})))
	t.Cleanup(srv.Close)
	source := testenv.TempDir(t)
	writePluginSource(t, source)

	install := func() map[string]any {
		t.Helper()
		preview := postJSON(t, srv.URL+"/plugins/plan", map[string]any{"source": source})
		defer preview.Body.Close()
		plan := decodeInstallSource(t, preview)
		if preview.StatusCode != http.StatusOK || plan["status"] != "planned" || plan["planId"] == nil {
			t.Fatalf("preview = %d: %v", preview.StatusCode, plan)
		}
		applied := postJSON(t, srv.URL+"/plugins/install", map[string]any{"source": source, "planId": plan["planId"]})
		defer applied.Body.Close()
		body := decodeInstallSource(t, applied)
		if applied.StatusCode != http.StatusOK {
			t.Fatalf("install = %d: %v", applied.StatusCode, body)
		}
		return body
	}
	if first := install(); first["status"] != "done" || first["ok"] != true || first["applied"] != true {
		t.Fatalf("first install = %v", first)
	}
	manifestPath := filepath.Join(pluginpkg.InstallRoot(home, "demo"), "reasonix-plugin.json")
	before, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	second := install()
	if second["ok"] != false || second["status"] != "failed" || second["applied"] != true {
		t.Fatalf("duplicate install = %v", second)
	}
	if got := second["reloadError"]; got != "cannot reload extensions while active work or background jobs are running" {
		t.Fatalf("reload refusal = %v", got)
	}
	actions, ok := second["actions"].([]any)
	if !ok || len(actions) != 1 {
		t.Fatalf("actions = %v", second["actions"])
	}
	failed := actions[0].(map[string]any)
	if failed["name"] != "demo" || failed["status"] != "failed" || failed["error"] == nil || failed["error"] == "" || failed["next"] == nil {
		t.Fatalf("failed action lost its diagnostic: %v", failed)
	}
	after, err := os.ReadFile(manifestPath)
	if err != nil || string(after) != string(before) {
		t.Fatalf("duplicate install changed the installed manifest: %v", err)
	}
	if list := getPlugins(t, srv.URL); len(list) != 1 || list[0].Name != "demo" || list[0].Version != "0.2.0" {
		t.Fatalf("inventory after duplicate install = %+v", list)
	}
}

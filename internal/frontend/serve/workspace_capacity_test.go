package serve

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/session/control"
)

func fullWorkspaceList(t *testing.T) []byte {
	t.Helper()
	for i := range workspaceRecentMax {
		rememberWorkspace(fmt.Sprintf("/project-%d", i))
	}
	data, err := os.ReadFile(workspacesPath())
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestHubOpenAtCapacityRefusesBeforePublishingRuntime(t *testing.T) {
	hubBootEnv(t)
	before := fullWorkspaceList(t)
	h := NewHub(HubOptions{})
	defer h.Shutdown()
	srv := httptest.NewServer(operatorHandler(h))
	defer srv.Close()
	status, body := openPane(t, srv, testenv.TempDir(t), "")
	if status != http.StatusConflict || refusalCodeOf(t, body) != "workspace.limit_reached" || len(h.List()) != 0 {
		t.Fatalf("hub open at capacity: status=%d body=%s runtimes=%d", status, body, len(h.List()))
	}
	after, err := os.ReadFile(workspacesPath())
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("refused hub open changed order or launch")
	}
	root := testenv.TempDir(t)
	forgetWorkspace("/project-0")
	if status, body := openPane(t, srv, root, ""); status != http.StatusOK {
		t.Fatalf("retry after removal: %d %s", status, body)
	}
	if len(h.List()) != 1 || LaunchWorkspaces()[0] != root {
		t.Fatal("retry did not publish and remember the new project")
	}
}

func TestWorkspaceSwitchAtCapacityKeepsOutgoingController(t *testing.T) {
	hubBootEnv(t)
	before := fullWorkspaceList(t)
	old := control.New(control.Options{})
	defer old.Close()
	replacement := control.New(control.Options{})
	defer replacement.Close()
	s := New(old, NewBroadcaster(), config.ServeConfig{})
	s.AllowWorkspaceSwitch()
	s.buildWorkspaceController = func(_ context.Context, _, _ string) (*control.Controller, error) { return replacement, nil }
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()
	body, _ := json.Marshal(map[string]string{"path": testenv.TempDir(t)})
	status, code := postLaunchJSON(t, srv.URL+"/workspace", string(body), nil)
	if status != http.StatusConflict || code != "workspace.limit_reached" || s.Controller() != control.SessionAPI(old) {
		t.Fatalf("switch at capacity: %d %s, outgoing preserved=%v", status, code, s.Controller() == control.SessionAPI(old))
	}
	after, err := os.ReadFile(workspacesPath())
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("refused switch changed order or launch")
	}
}

func TestWorkspaceRepairsKeepEveryOriginalBackup(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	originals := []string{"first damaged list", `{"paths":42}`}
	for _, original := range originals {
		if err := os.WriteFile(workspacesPath(), []byte(original), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := addRememberedWorkspace(t.Context(), "/new"); err != nil {
			t.Fatal(err)
		}
	}
	backups, err := filepath.Glob(workspacesPath() + ".bak-*")
	if err != nil || len(backups) != len(originals) {
		t.Fatalf("backups=%v, %v", backups, err)
	}
	saved := map[string]bool{}
	for _, backup := range backups {
		data, err := os.ReadFile(backup)
		if err != nil {
			t.Fatal(err)
		}
		saved[string(data)] = true
	}
	for _, original := range originals {
		if !saved[original] {
			t.Fatalf("lost original %q", original)
		}
	}
}

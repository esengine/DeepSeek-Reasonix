package serve

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/session/control"
)

// A server whose host did not grant the switch must not be repointable, however
// the request is spelled: reaching any directory on the machine is the whole
// risk the flag exists to hold back.
func TestWorkspaceSwitchRefusedWithoutGrant(t *testing.T) {
	ctrl := control.New(control.Options{})
	defer ctrl.Close()
	srv := httptest.NewServer(operatorHandler(New(ctrl, NewBroadcaster(), config.ServeConfig{})))
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/workspace", "application/json",
		strings.NewReader(`{"path":"`+jsonPath(testenv.TempDir(t))+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("POST /workspace = %d, want 403", resp.StatusCode)
	}
}

func TestWorkspaceSwitchRebuildsAtNewRoot(t *testing.T) {
	dir := testenv.TempDir(t)
	old := control.New(control.Options{})
	replacement := control.New(control.Options{})
	var gotDir string

	s := New(old, NewBroadcaster(), config.ServeConfig{})
	s.AllowWorkspaceSwitch()
	s.buildWorkspaceController = func(_ context.Context, d, _ string) (*control.Controller, error) {
		gotDir = d
		return replacement, nil
	}
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()
	defer replacement.Close()

	resp, err := http.Post(srv.URL+"/workspace", "application/json",
		strings.NewReader(`{"path":"`+jsonPath(dir)+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /workspace = %d, want 200", resp.StatusCode)
	}
	var body struct {
		WorkspaceRoot string `json:"workspaceRoot"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}

	want, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	if gotDir != want {
		t.Errorf("built at %q, want %q", gotDir, want)
	}
	if body.WorkspaceRoot != want {
		t.Errorf("reported root %q, want %q", body.WorkspaceRoot, want)
	}
	if s.Controller() != control.SessionAPI(replacement) {
		t.Error("the server kept serving the outgoing controller")
	}
}

func TestWorkspaceSwitchRejectsMissingDirectory(t *testing.T) {
	ctrl := control.New(control.Options{})
	defer ctrl.Close()
	s := New(ctrl, NewBroadcaster(), config.ServeConfig{})
	s.AllowWorkspaceSwitch()
	s.buildWorkspaceController = func(context.Context, string, string) (*control.Controller, error) {
		t.Fatal("a build was attempted for a path that does not exist")
		return nil, nil
	}
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()

	missing := filepath.Join(testenv.TempDir(t), "not-here")
	resp, err := http.Post(srv.URL+"/workspace", "application/json",
		strings.NewReader(`{"path":"`+jsonPath(missing)+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("POST /workspace = %d, want 400", resp.StatusCode)
	}
}

// Windows paths carry backslashes, which are escapes inside a JSON string.
func jsonPath(p string) string {
	b, _ := json.Marshal(p)
	return strings.Trim(string(b), `"`)
}

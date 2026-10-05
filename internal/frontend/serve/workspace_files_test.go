package serve

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/session/control"
)

func TestWorkspaceFileEditRequiresTheRevisionItRead(t *testing.T) {
	dir := testenv.TempDir(t)
	path := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(path, []byte("one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	bc := NewBroadcaster()
	ctrl := control.New(control.Options{Runner: fakeRunner{}, Sink: bc, WorkspaceRoot: dir})
	srv := httptest.NewServer(operatorHandler(New(ctrl, bc, config.ServeConfig{})))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/workspace/file?path=note.txt")
	if err != nil {
		t.Fatal(err)
	}
	var file workspaceFile
	if err := json.NewDecoder(resp.Body).Decode(&file); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	file.Content = "two\n"
	body, _ := json.Marshal(file)
	put := func() (*http.Response, error) {
		req, _ := http.NewRequest(http.MethodPut, srv.URL+"/workspace/file", bytes.NewReader(body))
		req.Header.Set("content-type", "application/json")
		return http.DefaultClient.Do(req)
	}
	resp, err = put()
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT = %v, %v", resp, err)
	}
	resp.Body.Close()
	if got, _ := os.ReadFile(path); string(got) != "two\n" {
		t.Fatalf("file = %q", got)
	}

	resp, err = put()
	if err != nil || resp.StatusCode != http.StatusConflict {
		t.Fatalf("stale PUT status = %v, %v; want 409", resp, err)
	}
	resp.Body.Close()
}

func TestWorkspaceFileRefusesPathsOutsideTheRoot(t *testing.T) {
	dir := testenv.TempDir(t)
	bc := NewBroadcaster()
	ctrl := control.New(control.Options{Runner: fakeRunner{}, Sink: bc, WorkspaceRoot: dir})
	srv := httptest.NewServer(operatorHandler(New(ctrl, bc, config.ServeConfig{})))
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/workspace/file?path=../secret.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("GET outside path = %d, want 400", resp.StatusCode)
	}
}
